package wolfios

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type Context struct {
	client   *http.Client
	indexURL string
}

const PackageIndex = "https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz"

func New(client *http.Client, indexURL string) Context {
	return Context{
		client:   client,
		indexURL: indexURL,
	}
}

func (c Context) GetWolfiPackages() (map[string]bool, error) {
	wolfiPackages := make(map[string]bool)

	req, _ := http.NewRequest("GET", c.indexURL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return wolfiPackages, errors.Wrapf(err, "failed getting URI %s", PackageIndex)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return wolfiPackages, fmt.Errorf("non ok http response for URI %s code: %v", PackageIndex, resp.StatusCode)
	}

	dir, err := os.MkdirTemp("", "wolfictl")
	if err != nil {
		return wolfiPackages, errors.Wrap(err, "failed creating temp dir")
	}
	defer os.RemoveAll(dir)

	err = Untar(dir, resp.Body)
	if err != nil {
		return wolfiPackages, errors.Wrap(err, "failed to untar wolfi index")
	}

	return parseIndex(filepath.Join(dir, "APKINDEX"))

}

func parseIndex(indexFile string) (map[string]bool, error) {
	wolfiPackages := make(map[string]bool)

	f, err := os.OpenFile(indexFile, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return wolfiPackages, errors.Wrapf(err, "failed to open index file %s", indexFile)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "P:") {
			wolfiPackages[line[2:]] = true
		}
	}
	if err := sc.Err(); err != nil {
		return wolfiPackages, errors.Wrapf(err, "failed to scan index file %s", indexFile)
	}

	return wolfiPackages, nil
}

// Untar takes a destination path and a reader; a tar reader loops over the tarfile
// creating the file structure at 'dst' along the way, and writing any files
func Untar(dst string, r io.Reader) error {

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}
