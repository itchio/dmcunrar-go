package dmcunrar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestCancelBeforeOpen(t *testing.T) {
	data, err := os.ReadFile(testdataPath(t, "simple.rar"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = OpenArchiveContext(ctx, bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("OpenArchiveContext with pre-canceled ctx returned nil error")
	}
	if !errors.Is(err, ErrUserCancel) {
		t.Fatalf("err = %v, want errors.Is(err, ErrUserCancel)", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want errors.Is(err, context.Canceled)", err)
	}

	var unrarErr *Error
	if !errors.As(err, &unrarErr) {
		t.Fatalf("err = %T, want wraps *Error", err)
	}
	if unrarErr.Code != ErrorCodeUserCancel {
		t.Fatalf("Code = %d, want ErrorCodeUserCancel (%d)", unrarErr.Code, ErrorCodeUserCancel)
	}
}

// cancelOnReadAt cancels its context on the first read after extraction
// has begun. Cancel is polled at library internal points, so triggering it
// during a read reliably causes the next poll to observe cancellation.
type cancelOnReadAt struct {
	inner  io.ReaderAt
	cancel context.CancelFunc
	armed  bool
}

func (r *cancelOnReadAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.inner.ReadAt(p, off)
	if r.armed && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return n, err
}

func TestCancelDuringExtract(t *testing.T) {
	// solid-long.rar has 8 files in a solid chain. Extracting the last
	// entry forces decompression of the entire chain, which triggers
	// many read and cancel-poll points. We arm the cancel *after* open
	// completes, and let the first extraction read trigger it.
	data, err := os.ReadFile(testdataPath(t, "solid-long.rar"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	wrapped := &cancelOnReadAt{
		inner:  bytes.NewReader(data),
		cancel: cancel,
	}
	archive, err := OpenArchiveContext(ctx, wrapped, int64(len(data)))
	if err != nil {
		t.Fatalf("OpenArchiveContext: %v", err)
	}
	defer archive.Free()

	count := archive.GetFileCount()
	if count < 2 {
		t.Fatalf("solid-long.rar file count = %d, want >= 2", count)
	}
	lastIndex := count - 1

	// Arm the reader to cancel on the next read (i.e., inside ExtractFile).
	wrapped.armed = true

	ef := NewExtractedFile(io.Discard)
	defer ef.Free()

	err = archive.ExtractFile(ef, lastIndex)
	if err == nil {
		t.Fatal("ExtractFile after cancel returned nil error")
	}
	if !errors.Is(err, ErrUserCancel) {
		t.Fatalf("err = %v, want errors.Is(err, ErrUserCancel)", err)
	}
}

func TestSetCancelContext(t *testing.T) {
	// Open without a context, attach one mid-life, then cancel and extract.
	data, err := os.ReadFile(testdataPath(t, "solid-long.rar"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	archive, err := OpenArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	defer archive.Free()

	ctx, cancel := context.WithCancel(context.Background())
	archive.SetCancelContext(ctx)
	cancel()

	ef := NewExtractedFile(io.Discard)
	defer ef.Free()

	err = archive.ExtractFile(ef, archive.GetFileCount()-1)
	if err == nil {
		t.Fatal("ExtractFile after SetCancelContext(canceled) returned nil error")
	}
	if !errors.Is(err, ErrUserCancel) {
		t.Fatalf("err = %v, want errors.Is(err, ErrUserCancel)", err)
	}
}

func TestSetCancelContextReplacement(t *testing.T) {
	// Regression: setting ctx2 after ctx1 must swap the live context. Before
	// the fix path was cleaned up, a later SetCancelContext could leave the
	// C-side callback mismatched with the Go-side ctx, silently ignoring
	// cancellation.
	data, err := os.ReadFile(testdataPath(t, "solid-long.rar"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	archive, err := OpenArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	defer archive.Free()

	// Attach a never-canceled ctx, replace with a background, then attach and
	// cancel a fresh one. The final ctx must take effect.
	ctx1, cancel1 := context.WithCancel(context.Background())
	archive.SetCancelContext(ctx1)
	archive.SetCancelContext(context.Background())
	cancel1() // must have no effect now

	ctx2, cancel2 := context.WithCancel(context.Background())
	archive.SetCancelContext(ctx2)
	cancel2()

	ef := NewExtractedFile(io.Discard)
	defer ef.Free()

	err = archive.ExtractFile(ef, archive.GetFileCount()-1)
	if err == nil {
		t.Fatal("ExtractFile after replacement+cancel returned nil error")
	}
	if !errors.Is(err, ErrUserCancel) {
		t.Fatalf("err = %v, want errors.Is(err, ErrUserCancel)", err)
	}
}

func TestCancelNotFiredOnNormalExtract(t *testing.T) {
	// A context attached but never canceled must not interfere with normal extraction.
	ctx := context.Background()
	archive, err := OpenArchiveFromPathContext(ctx, testdataPath(t, "simple.rar"))
	if err != nil {
		t.Fatalf("OpenArchiveFromPathContext: %v", err)
	}
	defer archive.Free()

	var buf bytes.Buffer
	ef := NewExtractedFile(&buf)
	defer ef.Free()

	if err := archive.ExtractFile(ef, 0); err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}
	if buf.String() != "Hello, World!" {
		t.Fatalf("extracted = %q, want %q", buf.String(), "Hello, World!")
	}
}
