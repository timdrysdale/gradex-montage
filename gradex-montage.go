package main

/*
 * Add a cover page to a PDF file
 * Generates cover page then merges, including form field data (AcroForms).
 *
 * Run as: gradex-coverpage <barefile>.pdf
 *
 * outputs: <barefile>-covered.pdf (using internally generated cover page)
 *
 * Adapted from github.com/unidoc/unipdf-examples/pages/pdf_merge_advanced.go
 *
 *
 */

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bsipos/thist"
	"github.com/timdrysdale/parsesvg"
	"github.com/timdrysdale/pdfcomment"
	"github.com/timdrysdale/pool"
	unicommon "github.com/unidoc/unipdf/v3/common"
	pdf "github.com/unidoc/unipdf/v3/model"
)

func init() {
	// Debug log level.
	unicommon.SetLogger(unicommon.NewConsoleLogger(unicommon.LogLevelInfo))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Requires two arguments: sidebar input_path[s]\n")
		fmt.Printf("Usage: gradex-overlay.exe sidebar input-*.pdf\n")
		os.Exit(0)
	}

	spreadName := os.Args[1]

	var inputPath []string

	inputPath = os.Args[2:]

	suffix := filepath.Ext(inputPath[0])

	// sanity check
	if suffix != ".pdf" {
		fmt.Printf("Error: input path must be a .pdf\n")
		os.Exit(1)
	}

	N := len(inputPath)

	pcChan := make(chan int, N)

	tasks := []*pool.Task{}

	for i := 0; i < N; i++ {

		inputPDF := inputPath[i]
		spreadName := spreadName
		newtask := pool.NewTask(func() error {
			pc, err := doOneDoc(inputPDF, spreadName)
			pcChan <- pc
			return err
		})
		tasks = append(tasks, newtask)
	}

	p := pool.NewPool(tasks, runtime.GOMAXPROCS(-1))

	closed := make(chan struct{})

	h := thist.NewHist(nil, "Page count", "fixed", 10, false)

	go func() {
	LOOP:
		for {
			select {
			case pc := <-pcChan:
				h.Update(float64(pc))
				fmt.Println(h.Draw())
			case <-closed:
				break LOOP
			}
		}
	}()

	p.Run()

	var numErrors int
	for _, task := range p.Tasks {
		if task.Err != nil {
			fmt.Println(task.Err)
			numErrors++
		}
	}
	close(closed)

}

func doOneDoc(inputPath, spreadName string) (int, error) {

	if strings.ToLower(filepath.Ext(inputPath)) != ".pdf" {
		return 0, errors.New(fmt.Sprintf("%s does not appear to be a pdf", inputPath))
	}

	// need page count to find the jpeg files again later
	numPages, err := countPages(inputPath)

	// render to images
	jpegPath := "./jpg"
	err = ensureDir(jpegPath)
	if err != nil {
		return 0, err
	}
	suffix := filepath.Ext(inputPath)
	basename := strings.TrimSuffix(inputPath, suffix)
	jpegFileOption := fmt.Sprintf("%s/%s%%04d.jpg", jpegPath, basename)

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Println("Can't open pdf")
		os.Exit(1)
	}

	pdfReader, err := pdf.NewPdfReader(f)
	if err != nil {
		fmt.Println("Can't read test pdf")
		os.Exit(1)
	}

	comments, err := pdfcomment.GetComments(pdfReader)

	f.Close()

	err = convertPDFToJPEGs(inputPath, jpegPath, jpegFileOption)
	if err != nil {
		return 0, err
	}

	// convert images to individual pdfs, with form overlay

	pagePath := "./pdf"
	err = ensureDir(pagePath)
	if err != nil {
		return 0, err
	}

	pageFileOption := fmt.Sprintf("%s/%s%%04d.pdf", pagePath, basename)

	mergePaths := []string{}

	// gs starts indexing at 1
	for imgIdx := 1; imgIdx <= numPages; imgIdx = imgIdx + 1 {

		// construct image name
		previousImagePath := fmt.Sprintf(jpegFileOption, imgIdx)
		pageFilename := fmt.Sprintf(pageFileOption, imgIdx)

		//TODO select Layout to suit landscape or portrait
		svgLayoutPath := "./test/layout-312pt-static-mark-dynamic-moderate-comment-static-check.svg"

		pageNumber := imgIdx - 1

		contents := parsesvg.SpreadContents{
			SvgLayoutPath:     svgLayoutPath,
			SpreadName:        spreadName,
			PreviousImagePath: previousImagePath,
			PageNumber:        pageNumber,
			PdfOutputPath:     pageFilename,
			Comments:          comments,
		}

		err := parsesvg.RenderSpreadExtra(contents)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		//	err := parsesvg.RenderSpread(svgLayoutPath, spreadName, previousImagePath, imgIdx, pageFilename)
		//	if err != nil {
		//		return 0, err
		//	}

		//save the pdf filename for the merge at the end
		mergePaths = append(mergePaths, pageFilename)
	}

	outputPath := fmt.Sprintf("%s-%s.pdf", basename, spreadName)
	err = mergePdf(mergePaths, outputPath)
	if err != nil {
		return 0, err
	}

	return numPages, nil

}
