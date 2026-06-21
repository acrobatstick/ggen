package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/disintegration/imaging"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type mediaformat string

const (
	jpgformat  mediaformat = ".jpg"
	pngformat  mediaformat = ".png"
	gifformat  mediaformat = ".gif"
	webpformat mediaformat = ".webp"
	webmformat mediaformat = ".webm"
	mp4format  mediaformat = ".mp4"
)

type Media struct {
	format mediaformat
	// relative path to media source
	Path string
	// base64 encoded preview of the media file
	Src string
}

// default amount of concurrent process worker
var numWorkers = runtime.NumCPU()

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	entries := getMedia(cwd)
	if len(entries) == 0 {
		fmt.Println("no media sources found in this directory")
		os.Exit(2)
	}

	computes := make(chan *Media, numWorkers*2)
	var wg sync.WaitGroup

	// spawn workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(computes, &wg)
	}

	// send job to workers
	for i := range entries {
		computes <- &entries[i]
	}

	close(computes)
	wg.Wait()
	if err := marshalPage(entries); err != nil {
		panic(err)
	}

	os.Exit(1)
}

func getMedia(src string) []Media {
	dirEntries, err := os.ReadDir(src)
	if err != nil {
		panic(err)
	}

	mediaEntries := []Media{}
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		ext := path.Ext(name)
		switch ext {
		case ".jpg":
			mediaEntries = append(mediaEntries, Media{format: jpgformat, Path: name})
		case ".png":
			mediaEntries = append(mediaEntries, Media{format: pngformat, Path: name})

		// UNIMPLEMENTED
		case ".gif":
			mediaEntries = append(mediaEntries, Media{format: gifformat, Path: name})
		case ".webp":
			mediaEntries = append(mediaEntries, Media{format: webpformat, Path: name})
		case ".webm":
			mediaEntries = append(mediaEntries, Media{format: webmformat, Path: name})
		case ".mp4":
			mediaEntries = append(mediaEntries, Media{format: mp4format, Path: name})
		}
	}

	// check for existing cache
	htmlpagePath := "gallery.html"
	_, err = os.Stat(htmlpagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return mediaEntries
		}
	}

	page, err := os.Open(htmlpagePath)
	if err != nil {
		panic(fmt.Sprintf("error opening html page file: %v", err))
	}
	defer page.Close()

	srcMap, err := collectPreviewSrcs(page)
	if err != nil {
		panic(fmt.Sprintf("error while collecting href on html file: %v", err))
	}

	for i := range mediaEntries {
		cur := mediaEntries[i]
		src, ok := srcMap[cur.Path]
		if !ok {
			continue
		}
		mediaEntries[i].Src = src
	}

	return mediaEntries
}

func worker(computes <-chan *Media, wg *sync.WaitGroup) {
	defer wg.Done()
	for media := range computes {
		if err := computePreview(media); err != nil {
			fmt.Printf("error generating preview image: %v", err)
			continue
		}
	}
}

// default height of the resized preview image is set to 150px
const defaultPreviewHeight = 150

func computePreview(media *Media) error {
	// skip if cached already
	// TODO: should not skip if height provided is different from the already generated page
	if len(media.Src) != 0 {
		return nil
	}

	switch media.format {
	case jpgformat, pngformat:
		img, err := imaging.Open(media.Path)
		if err != nil {
			return err
		}

		img = resizeImage(img, defaultPreviewHeight) // TODO: make prev height configurable
		buf := new(bytes.Buffer)

		if media.format == jpgformat {
			if err := jpeg.Encode(buf, img, nil); err != nil {
				return err
			}

			encoded, err := encodeImage(buf)
			if err != nil {
				return err
			}
			media.Src = fmt.Sprintf("data:image/jpeg;base64,%s", encoded)
		} else {
			if err := png.Encode(buf, img); err != nil {
				return err
			}
			encoded, err := encodeImage(buf)
			if err != nil {
				return err
			}
			media.Src = fmt.Sprintf("data:image/png;base64,%s", encoded)
		}
	}

	return nil
}

// encodes image's buffer into base64 string
func encodeImage(buf *bytes.Buffer) (string, error) {
	var out bytes.Buffer
	enc := base64.NewEncoder(base64.StdEncoding, &out)
	_, err := io.Copy(enc, buf)
	if err != nil {
		return "", err
	}
	enc.Close()

	return out.String(), nil
}

func resizeImage(src image.Image, height int) image.Image {
	dy := src.Bounds().Dy()

	// resize if the current image is higher than the target height
	if height < dy {
		return imaging.Resize(src, 0, height, imaging.Lanczos)
	}

	return src
}

func collectPreviewSrcs(r *os.File) (map[string]string, error) {
	node, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	m := make(map[string]string)

	for n := range node.Descendants() {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			var path string
			for _, a := range n.Attr {
				if a.Key == "href" {
					path = strings.TrimLeft(a.Val, "./")
					break
				}
			}

			for c := range n.Descendants() {
				if c.Type == html.ElementNode && c.DataAtom == atom.Img {
					for _, a := range c.Attr {
						if a.Key == "src" {
							m[path] = a.Val
						}
					}
				}
			}
		}
	}

	return m, nil
}

func marshalPage(entries []Media) error {
	const tpl = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>My Galery</title>
	</head>
	<body>
		{{range .Items}}
		<a href="./{{.Path}}" target="_blank">
			<img src="{{.Src}}"/>
		</a>
		{{else}}
			<div><strong>no rows</strong></div>
		{{end}}
	</body>
</html>`

	t, err := template.New("gallery").Parse(tpl)
	if err != nil {
		return err
	}

	data := struct {
		Items []Media
	}{
		Items: entries,
	}

	f, err := os.OpenFile(fmt.Sprintf("%s.html", "gallery"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)

	if err != nil {
		return err
	}
	defer f.Close()

	err = t.Execute(f, data)
	if err != nil {
		return err
	}

	return nil
}
