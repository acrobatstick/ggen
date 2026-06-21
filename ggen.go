package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/disintegration/imaging"
	"github.com/urfave/cli/v3"
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

type resultStatus int

const (
	resultSuccess resultStatus = iota
	resultFailed
	resultCached
)

type result struct {
	// the media file path
	path   string
	status resultStatus
}

func main() {
	cmd := cli.Command{
		Name:      "ggen",
		Usage:     "generate image mood board from a directory as html static file",
		UsageText: "ggen [global options] <directory path>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "procs",
				Aliases: []string{"p"},
				Usage:   "number of process for the concurrent worker",
				Value:   runtime.NumCPU(),
			},
			&cli.BoolFlag{
				Name:    "open",
				Aliases: []string{"o"},
				Usage:   "open the generated html file in your default browser",
				Value:   false,
			},
		},
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:      "path",
				UsageText: "<folder path> (default: current working directory)",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return run(c)
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		panic(err)
	}
}

func run(c *cli.Command) error {
	var err error
	var p string

	p = c.StringArg("path")
	p, err = resolvePath(p)
	if err != nil {
		return fmt.Errorf("error while resolving path: %v", err)
	}

	entries := getMedia(p)
	if len(entries) == 0 {
		return fmt.Errorf("no media sources found in this directory\n")
	}

	procs := c.Int("procs")

	computes := make(chan *Media, procs*2)
	results := make(chan result, procs*2)

	var wg sync.WaitGroup

	// spawn workers
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go worker(computes, results, &wg)
	}

	// send job to workers
	for i := range entries {
		computes <- &entries[i]
	}

	close(computes)
	go func() {
		defer close(results)
		wg.Wait()
	}()

	for res := range results {
		switch res.status {
		case resultSuccess:
			fmt.Printf("Media processed successfully: %s\n", res.path)

		case resultFailed:
			fmt.Printf("Failed to process media: %s\n", res.path)

		case resultCached:
			fmt.Printf("Media already cached, skipping processing: %s\n", res.path)
		}
	}

	base := path.Base(p)
	if err := marshalPage(base, entries); err != nil {
		return fmt.Errorf("error while marshaling html page: %v", err)
	}

	open := c.Bool("open")
	if open {
		url := fmt.Sprintf("file://%s/%s.html", p, base)
		return openURL(url)
	}

	return nil
}

// openURL opens the specified URL in the default browser of the user.
func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd.exe"
		args = []string{"/c", "rundll32", "url.dll,FileProtocolHandler", strings.ReplaceAll(url, "&", "^&")}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		if isWSL() {
			cmd = "cmd.exe"
			args = []string{"start", url}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	}

	e := exec.Command(cmd, args...)
	err := e.Start()
	if err != nil {
		return err
	}
	err = e.Wait()
	if err != nil {
		return err
	}

	return nil
}

// isWSL checks if the Go program is running inside Windows Subsystem for Linux
func isWSL() bool {
	releaseData, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(releaseData)), "microsoft")
}

func resolvePath(target string) (string, error) {
	p := target
	if target == "." || target == "" {
		dir, err := os.Getwd()
		if err != nil {
			return "", err
		}
		p = dir
	} else {
		if p == "~" {
			return os.UserHomeDir()
		}

		if strings.HasPrefix(p, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			p = path.Join(home, p[2:])
		}

		var err error
		p, err = filepath.Abs(p)
		if err != nil {
			return "", err
		}

		fi, err := os.Stat(p)
		if err != nil {
			return "", err
		}

		if !fi.IsDir() {
			return "", fmt.Errorf("%q is not a directory", p)
		}
	}

	return p, nil
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
	htmlpagePath := fmt.Sprintf("%s.html", path.Base(src))
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

func worker(computes <-chan *Media, results chan<- result, wg *sync.WaitGroup) {
	defer wg.Done()
	for media := range computes {
		resCode, err := computePreview(media)

		if err != nil {
			fmt.Printf("error generating preview image: %v", err)
			results <- result{path: media.Path, status: resCode}
			continue
		}

		results <- result{path: media.Path, status: resCode}
	}
}

// default height of the resized preview image is set to 150px
const defaultPreviewHeight = 150

func computePreview(media *Media) (resultStatus, error) {
	// skip if cached already
	// TODO: should not skip if height provided is different from the already generated page
	if len(media.Src) != 0 {
		return resultCached, nil
	}

	switch media.format {
	case jpgformat, pngformat:
		img, err := imaging.Open(media.Path)
		if err != nil {
			return resultFailed, err
		}

		img = resizeImage(img, defaultPreviewHeight) // TODO: make prev height configurable
		buf := new(bytes.Buffer)

		if media.format == jpgformat {
			if err := jpeg.Encode(buf, img, nil); err != nil {
				return resultFailed, err
			}

			encoded, err := encodeImage(buf)
			if err != nil {
				return resultFailed, err
			}
			media.Src = fmt.Sprintf("data:image/jpeg;base64,%s", encoded)
		} else {
			if err := png.Encode(buf, img); err != nil {
				return resultFailed, err
			}
			encoded, err := encodeImage(buf)
			if err != nil {
				return resultFailed, err
			}
			media.Src = fmt.Sprintf("data:image/png;base64,%s", encoded)
		}
	}

	return resultSuccess, nil
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

func marshalPage(fname string, entries []Media) error {
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

	f, err := os.OpenFile(fmt.Sprintf("%s.html", fname), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)

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
