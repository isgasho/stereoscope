package file

import (
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"path"

	"github.com/pkg/errors"

	"github.com/anchore/stereoscope/internal"
	"github.com/anchore/stereoscope/internal/log"
)

// tarFile is a ReadCloser of a tar file on disk.
type tarFile struct {
	io.Reader
	io.Closer
}

// TarContentsRequest is a map of tarHeaderNames -> file.References to aid in simplifying content retrieval.
type TarContentsRequest map[string]Reference

// ReaderFromTar returns a io.ReadCloser for the path within a tar file.
func ReaderFromTar(reader io.ReadCloser, tarPath string) (io.ReadCloser, error) {
	tarReader := tar.NewReader(reader)
	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == tarPath {
			return tarFile{
				Reader: tarReader,
				Closer: reader,
			}, nil
		}
	}
	return nil, fmt.Errorf("file %s not found in tar", tarPath)
}

// ContentsFromTar reads the contents of a tar for the selection of tarHeaderNames, where the return is a mapping of the file reference from the original request to the fetched contents.
func ContentsFromTar(reader io.Reader, tarHeaderNames TarContentsRequest) (map[Reference]string, error) {
	result := make(map[Reference]string)
	tarReader := tar.NewReader(reader)

	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if fileRef, ok := tarHeaderNames[hdr.Name]; ok {
			bytes, err := ioutil.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("could not read file: %s: %+v", hdr.Name, err)
			}
			result[fileRef] = string(bytes)
		}
	}

	if len(result) != len(tarHeaderNames) {
		resultSet := internal.NewStringSet()
		missingNames := make([]string, 0)
		for _, name := range result {
			resultSet.Add(name)
		}
		for name := range tarHeaderNames {
			if !resultSet.Contains(name) {
				missingNames = append(missingNames, name)
			}
		}
		return nil, fmt.Errorf("not all files found: %+v", missingNames)
	}

	return result, nil
}

// EnumerateFileMetadataFromTar populates and returns a Metadata object for all files in the tar.
func EnumerateFileMetadataFromTar(reader io.Reader) <-chan Metadata {
	tarReader := tar.NewReader(reader)
	result := make(chan Metadata)
	go func() {
		for {
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				log.Errorf("failed to read next tar header: %w", err)
				return
			}

			// always ensure relative path notations are not parsed as part of the filename
			name := path.Clean(DirSeparator + header.Name)
			if name == "." {
				continue
			}

			switch header.Typeflag {
			case tar.TypeXGlobalHeader:
				log.Errorf("unexpected tar file: (XGlobalHeader): type=%v name=%s", header.Typeflag, name)
			case tar.TypeXHeader:
				log.Errorf("unexpected tar file (XHeader): type=%v name=%s", header.Typeflag, name)
			default:
				result <- Metadata{
					Path:          name,
					TarHeaderName: header.Name,
					TypeFlag:      header.Typeflag,
					Linkname:      header.Linkname,
					Size:          header.FileInfo().Size(),
					Mode:          header.FileInfo().Mode(),
					UserID:        header.Uid,
					GroupID:       header.Gid,
					IsDir:         header.FileInfo().IsDir(),
				}
			}
		}
		close(result)
	}()
	return result
}
