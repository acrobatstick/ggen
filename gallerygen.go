package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path"
	"text/template"

	"github.com/disintegration/imaging"
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

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	entries, err := os.ReadDir(cwd)
	if err != nil {
		panic(err)
	}

	mediaEntries := []Media{}

	for _, e := range entries {
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

	// compute media previews
	for i := range mediaEntries {
		computePreview(&mediaEntries[i])
	}

	if err := marshalPage(mediaEntries); err != nil {
		panic(err)
	}
}

const defaultPreviewHeight = 150

func computePreview(media *Media) error {
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
			media.Src = fmt.Sprintf("data:image/jpeg;base64,%s", bufToBase64(buf))
		} else {
			if err := png.Encode(buf, img); err != nil {
				return err
			}
			media.Src = fmt.Sprintf("data:image/png;base64,%s", bufToBase64(buf))
		}
	}

	return nil
}

func bufToBase64(buf *bytes.Buffer) string {
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func resizeImage(src image.Image, height int) image.Image {
	dy := src.Bounds().Dy()

	// resize if the current image is higher than the target height
	if height < dy {
		return imaging.Resize(src, 0, height, imaging.Lanczos)
	}

	return src
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
