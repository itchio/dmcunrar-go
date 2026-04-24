package dmcunrar

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

type errWriter struct {
	err error
}

func (w errWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestExtractWriterError(t *testing.T) {
	archive := openTestArchive(t, "simple.rar")

	diskFull := errors.New("disk full")
	ef := NewExtractedFile(errWriter{err: diskFull})
	defer ef.Free()

	err := archive.ExtractFile(ef, 0)
	if err == nil {
		t.Fatal("ExtractFile with failing writer returned nil error")
	}
	if !errors.Is(err, diskFull) {
		t.Fatalf("err = %v, want errors.Is(err, diskFull)", err)
	}

	var unrarErr *Error
	if !errors.As(err, &unrarErr) {
		t.Fatalf("err = %T, want wraps *Error", err)
	}
	if unrarErr.Code != ErrorCodeWriteFail {
		t.Fatalf("Code = %d, want ErrorCodeWriteFail (%d)", unrarErr.Code, ErrorCodeWriteFail)
	}
}

func TestExtractShortWriter(t *testing.T) {
	archive := openTestArchive(t, "simple.rar")

	ef := NewExtractedFile(shortWriter{})
	defer ef.Free()

	err := archive.ExtractFile(ef, 0)
	if err == nil {
		t.Fatal("ExtractFile with short writer returned nil error")
	}
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("err = %v, want errors.Is(err, io.ErrShortWrite)", err)
	}
}

func TestExtractSuccessClearsStaleErr(t *testing.T) {
	// Two extractions with the same ExtractedFile: the first fails (bad writer),
	// the second uses a good writer and must succeed without leaking stale err.
	archive := openTestArchive(t, "multiple_files.rar")

	badEF := NewExtractedFile(errWriter{err: errors.New("boom")})
	if err := archive.ExtractFile(badEF, 0); err == nil {
		t.Fatal("expected first ExtractFile to fail")
	}
	badEF.Free()

	var buf bytes.Buffer
	ef := NewExtractedFile(&buf)
	defer ef.Free()
	if err := archive.ExtractFile(ef, 1); err != nil {
		t.Fatalf("second ExtractFile: %v", err)
	}
	if buf.String() != "Content B" {
		t.Fatalf("content = %q, want %q", buf.String(), "Content B")
	}
}

func TestTruncatedArchive(t *testing.T) {
	data, err := os.ReadFile(testdataPath(t, "compressed.rar"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	truncated := data[:len(data)/2]

	_, err = OpenArchive(bytes.NewReader(truncated), int64(len(truncated)))
	if err == nil {
		t.Fatal("OpenArchive on truncated data returned nil error")
	}
}

func TestSeekBounds(t *testing.T) {
	data := []byte("test data for seeking")
	reader := bytes.NewReader(data)
	fr := NewFileReader(reader, int64(len(data)))
	defer fr.Free()

	// seek exactly to EOF — valid
	if n, err := fr.Seek(int64(len(data)), io.SeekStart); err != nil {
		t.Fatalf("seek to EOF: err = %v", err)
	} else if n != int64(len(data)) {
		t.Fatalf("seek to EOF: n = %d, want %d", n, len(data))
	}

	// negative offset — error
	if _, err := fr.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("seek to -1: err = nil, want error")
	}

	// past size — error
	if _, err := fr.Seek(int64(len(data)+1), io.SeekStart); err == nil {
		t.Fatal("seek past size: err = nil, want error")
	}

	// SeekCurrent going negative — error
	if _, err := fr.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek reset: %v", err)
	}
	if _, err := fr.Seek(-5, io.SeekCurrent); err == nil {
		t.Fatal("seek to negative via SeekCurrent: err = nil, want error")
	}
}
