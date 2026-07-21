package pluginservice

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"time"
)

var artifactEpoch = time.Unix(0, 0).UTC()

func newDeterministicArchive(target io.Writer) (*gzip.Writer, *tar.Writer) {
	gzipWriter := gzip.NewWriter(target)
	gzipWriter.Header.ModTime = artifactEpoch
	gzipWriter.Header.OS = 255
	return gzipWriter, tar.NewWriter(gzipWriter)
}

func deterministicFileHeader(name string, info fs.FileInfo) *tar.Header {
	mode := int64(0o644)
	if info.Mode().Perm()&0o111 != 0 {
		mode = 0o755
	}
	return &tar.Header{
		Name: name, Mode: mode, Size: info.Size(), Typeflag: tar.TypeReg,
		ModTime: artifactEpoch, AccessTime: time.Time{}, ChangeTime: time.Time{},
		Uid: 0, Gid: 0, Uname: "", Gname: "", Format: tar.FormatUSTAR,
	}
}
