package dmcunrar

// This package uses the dmc_unrar library defaults for resource caps
// (MAX_FILE_COUNT, MAX_FILE_SIZE, MAX_TOTAL_SIZE, MAX_DICT_SIZE,
// MAX_COMPRESSION_RATIO, MAX_PPMD_SIZE_MB). Consumers that want tighter
// limits should set their own -D flags via a cgo CFLAGS directive in a
// file in their own package.

/*
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include "dmc_unrar.h"

// cgo gateway functions defined in glue.c
dmc_unrar_io_handler *dmc_unrar_go_get_handler(void);
bool efCallbackGo_cgo(void *opaque, void **buffer, uint64_t *buffer_size, uint64_t uncompressed_size, dmc_unrar_return *err);
bool cancelGo_cgo(void *opaque);
void dmc_unrar_go_set_cancel(dmc_unrar_archive *archive, dmc_unrar_cancel_func func, void *opaque);

typedef struct fr_opaque_tag {
	int64_t id;
} fr_opaque;

typedef struct ef_opaque_tag {
	int64_t id;
} ef_opaque;

typedef struct cancel_opaque_tag {
	int64_t id;
} cancel_opaque;
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"
)

type FileReader struct {
	id     int64
	reader io.ReaderAt
	offset int64
	size   int64
	opaque *C.fr_opaque
	err    error
}

type ExtractedFile struct {
	id     int64
	writer io.Writer
	opaque *C.ef_opaque
	err    error
}

type Archive struct {
	fr      *FileReader
	archive *C.dmc_unrar_archive
	cancel  *cancelState
}

type UnrarFile struct {
	cFile *C.dmc_unrar_file
}

type cancelState struct {
	id     int64
	opaque *C.cancel_opaque
	mu     sync.Mutex
	ctx    context.Context
}

type ErrorCode int

const (
	ErrorCodeOK                          ErrorCode = ErrorCode(C.DMC_UNRAR_OK)
	ErrorCodeReadFail                    ErrorCode = ErrorCode(C.DMC_UNRAR_READ_FAIL)
	ErrorCodeWriteFail                   ErrorCode = ErrorCode(C.DMC_UNRAR_WRITE_FAIL)
	ErrorCodeSeekFail                    ErrorCode = ErrorCode(C.DMC_UNRAR_SEEK_FAIL)
	ErrorCodeInvalidData                 ErrorCode = ErrorCode(C.DMC_UNRAR_INVALID_DATA)
	ErrorCodeArchiveUnsupportedEncrypted ErrorCode = ErrorCode(C.DMC_UNRAR_ARCHIVE_UNSUPPORTED_ENCRYPTED)
	ErrorCodeFileUnsupportedEncrypted    ErrorCode = ErrorCode(C.DMC_UNRAR_FILE_UNSUPPORTED_ENCRYPTED)
	ErrorCodeFileCRC32Fail               ErrorCode = ErrorCode(C.DMC_UNRAR_FILE_CRC32_FAIL)
	ErrorCodeUserCancel                  ErrorCode = ErrorCode(C.DMC_UNRAR_USER_CANCEL)
)

var (
	ErrEncrypted  = errors.New("rar data is encrypted")
	ErrUserCancel = errors.New("rar operation canceled")
)

type Error struct {
	Operation string
	Code      ErrorCode
	Message   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: error %d: %s", e.Operation, e.Code, e.Message)
}

func (e *Error) Is(target error) bool {
	switch target {
	case ErrEncrypted:
		return e.IsEncrypted()
	case ErrUserCancel:
		return e.Code == ErrorCodeUserCancel
	}
	return false
}

func (e *Error) IsEncrypted() bool {
	switch e.Code {
	case ErrorCodeArchiveUnsupportedEncrypted, ErrorCodeFileUnsupportedEncrypted:
		return true
	default:
		return false
	}
}

func OpenArchiveFromPath(name string) (*Archive, error) {
	f, stats, err := openAndStat(name)
	if err != nil {
		return nil, err
	}
	return openArchive(f, stats.Size(), nil)
}

func OpenArchiveFromPathContext(ctx context.Context, name string) (*Archive, error) {
	if ctx == nil {
		panic("dmcunrar: OpenArchiveFromPathContext called with nil context")
	}
	f, stats, err := openAndStat(name)
	if err != nil {
		return nil, err
	}
	return openArchive(f, stats.Size(), newCancelState(ctx))
}

func openAndStat(name string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("open archive: %w", err)
	}
	stats, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("stat archive: %w", err)
	}
	return f, stats, nil
}

func OpenArchive(reader io.ReaderAt, size int64) (*Archive, error) {
	return openArchive(reader, size, nil)
}

func OpenArchiveContext(ctx context.Context, reader io.ReaderAt, size int64) (*Archive, error) {
	if ctx == nil {
		panic("dmcunrar: OpenArchiveContext called with nil context")
	}
	return openArchive(reader, size, newCancelState(ctx))
}

func openArchive(reader io.ReaderAt, size int64, cs *cancelState) (*Archive, error) {
	fr := NewFileReader(reader, size)
	success := false
	defer func() {
		if !success {
			fr.Free()
			if cs != nil {
				cs.Free()
			}
		}
	}()

	a, err := openArchiveInternal(fr, cs)
	if err != nil {
		return nil, err
	}

	success = true
	return a, nil
}

func openArchiveInternal(fr *FileReader, cs *cancelState) (*Archive, error) {
	archive := (*C.dmc_unrar_archive)(C.malloc(C.sizeof_dmc_unrar_archive))
	success := false
	defer func() {
		if !success {
			C.free(unsafe.Pointer(archive))
		}
	}()

	if err := checkError("dmc_unrar_archive_init", C.dmc_unrar_archive_init(archive)); err != nil {
		return nil, err
	}

	archive.io.funcs = C.dmc_unrar_go_get_handler()
	archive.io.opaque = unsafe.Pointer(fr.opaque)
	archive.io.size = C.dmc_unrar_size_t(fr.size)

	if cs != nil {
		C.dmc_unrar_go_set_cancel(archive,
			(C.dmc_unrar_cancel_func)(unsafe.Pointer(C.cancelGo_cgo)),
			unsafe.Pointer(cs.opaque))
	}

	if err := checkError("dmc_unrar_archive_open", C.dmc_unrar_archive_open(archive)); err != nil {
		return nil, annotateCancel(err, cs)
	}

	a := &Archive{
		fr:      fr,
		archive: archive,
		cancel:  cs,
	}

	success = true
	return a, nil
}

func (a *Archive) Free() {
	if a.archive != nil {
		C.dmc_unrar_archive_close(a.archive)
		C.free(unsafe.Pointer(a.archive))
		a.archive = nil
	}

	if a.fr != nil {
		a.fr.Free()
		a.fr = nil
	}

	if a.cancel != nil {
		a.cancel.Free()
		a.cancel = nil
	}
}

// SetCancelContext installs or replaces the context used for cooperative
// cancellation. ctx must not be nil; pass context.Background() for "no
// cancellation". Safe to call concurrently with an in-flight cancel poll
// on the same archive; the swap is synchronized internally.
func (a *Archive) SetCancelContext(ctx context.Context) {
	if ctx == nil {
		panic("dmcunrar: SetCancelContext called with nil context")
	}
	if a.archive == nil {
		panic("dmcunrar: SetCancelContext called on freed archive")
	}
	if a.cancel == nil {
		a.cancel = newCancelState(ctx)
		C.dmc_unrar_go_set_cancel(a.archive,
			(C.dmc_unrar_cancel_func)(unsafe.Pointer(C.cancelGo_cgo)),
			unsafe.Pointer(a.cancel.opaque))
		return
	}
	a.cancel.setCtx(ctx)
}

func (a *Archive) GetFileCount() int64 {
	return int64(C.dmc_unrar_get_file_count(a.archive))
}

func (a *Archive) GetFilename(i int64) (string, error) {
	size := C.dmc_unrar_get_filename(
		a.archive,
		C.dmc_unrar_size_t(i),
		(*C.char)(nil),
		0,
	)
	if size == 0 {
		return "", fmt.Errorf("0-length filename for entry %d", i)
	}

	filename := (*C.char)(C.malloc(C.size_t(size)))
	defer C.free(unsafe.Pointer(filename))
	size = C.dmc_unrar_get_filename(
		a.archive,
		C.dmc_unrar_size_t(i),
		filename,
		size,
	)
	if size == 0 {
		return "", fmt.Errorf("0-length filename for entry %d", i)
	}

	C.dmc_unrar_unicode_make_valid_utf8(filename)
	if *filename == 0 {
		return "", fmt.Errorf("0-length filename (after make_valid_utf8) for entry %d", i)
	}

	return C.GoString(filename), nil
}

func (a *Archive) GetFileStat(i int64) *UnrarFile {
	return &UnrarFile{cFile: C.dmc_unrar_get_file_stat(a.archive, C.dmc_unrar_size_t(i))}
}

func (a *Archive) FileIsDirectory(i int64) bool {
	return bool(C.dmc_unrar_file_is_directory(a.archive, C.dmc_unrar_size_t(i)))
}

func (a *Archive) FileIsSupported(i int64) error {
	return checkError("dmc_unrar_file_is_supported", C.dmc_unrar_file_is_supported(a.archive, C.dmc_unrar_size_t(i)))
}

func (uf *UnrarFile) GetUncompressedSize() int64 {
	return int64(uf.cFile.uncompressed_size)
}

func (a *Archive) ExtractFile(ef *ExtractedFile, index int64) error {
	ef.err = nil
	a.fr.err = nil

	const bufferSize = 256 * 1024
	buffer := unsafe.Pointer(C.malloc(C.size_t(bufferSize)))
	defer C.free(buffer)

	cErr := checkError("dmc_unrar_extract_file_with_callback", C.dmc_unrar_extract_file_with_callback(
		a.archive,
		C.dmc_unrar_size_t(index),
		buffer,
		C.dmc_unrar_size_t(bufferSize),
		nil,
		C.bool(true),
		unsafe.Pointer(ef.opaque),
		(C.dmc_unrar_extract_callback_func)(unsafe.Pointer(C.efCallbackGo_cgo)),
	))

	if cErr != nil {
		if ef.err != nil {
			return fmt.Errorf("%w: writer: %w", cErr, ef.err)
		}
		if a.fr.err != nil {
			return fmt.Errorf("%w: reader: %w", cErr, a.fr.err)
		}
		return annotateCancel(cErr, a.cancel)
	}

	if ef.err != nil {
		return fmt.Errorf("extract: writer error (C returned OK): %w", ef.err)
	}
	if a.fr.err != nil {
		return fmt.Errorf("extract: reader error (C returned OK): %w", a.fr.err)
	}
	return nil
}

func NewFileReader(reader io.ReaderAt, size int64) *FileReader {
	opaque := (*C.fr_opaque)(C.malloc(C.sizeof_fr_opaque))

	fr := &FileReader{
		reader: reader,
		offset: 0,
		size:   size,
		opaque: opaque,
	}
	reserveFrId(fr)

	return fr
}

func NewExtractedFile(writer io.Writer) *ExtractedFile {
	opaque := (*C.ef_opaque)(C.malloc(C.sizeof_ef_opaque))

	ef := &ExtractedFile{
		writer: writer,
		opaque: opaque,
	}
	reserveEfId(ef)

	return ef
}

func newCancelState(ctx context.Context) *cancelState {
	opaque := (*C.cancel_opaque)(C.malloc(C.sizeof_cancel_opaque))
	cs := &cancelState{
		ctx:    ctx,
		opaque: opaque,
	}
	reserveCancelId(cs)
	return cs
}

func (cs *cancelState) setCtx(ctx context.Context) {
	cs.mu.Lock()
	cs.ctx = ctx
	cs.mu.Unlock()
}

func (cs *cancelState) getCtx() context.Context {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.ctx
}

func (cs *cancelState) Free() {
	if cs.id > 0 {
		freeCancelId(cs.id)
		cs.id = 0
	}
	if cs.opaque != nil {
		C.free(unsafe.Pointer(cs.opaque))
		cs.opaque = nil
	}
}

// annotateCancel wraps err with the Go-side context cause when the C library
// returned DMC_UNRAR_USER_CANCEL. Leaves err unchanged otherwise.
func annotateCancel(err error, cs *cancelState) error {
	if err == nil || cs == nil {
		return err
	}
	var unrarErr *Error
	if !errors.As(err, &unrarErr) || unrarErr.Code != ErrorCodeUserCancel {
		return err
	}
	ctx := cs.getCtx()
	if ctx == nil {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", err, ctxErr)
	}
	return err
}

func (fr *FileReader) Seek(offset int64, whence int) (int64, error) {
	var final int64
	switch whence {
	case io.SeekStart:
		final = offset
	case io.SeekCurrent:
		final = fr.offset + offset
	case io.SeekEnd:
		final = fr.size + offset
	default:
		return fr.offset, fmt.Errorf("invalid whence %d", whence)
	}

	if final < 0 || final > fr.size {
		return fr.offset, fmt.Errorf("seek out of range: %d (size=%d)", final, fr.size)
	}

	fr.offset = final
	return fr.offset, nil
}

func (fr *FileReader) Free() {
	if fr.id > 0 {
		freeFrId(fr.id)
		fr.id = 0
	}

	if fr.opaque != nil {
		C.free(unsafe.Pointer(fr.opaque))
		fr.opaque = nil
	}
}

func (ef *ExtractedFile) Free() {
	if ef.id > 0 {
		freeEfId(ef.id)
		ef.id = 0
	}

	if ef.opaque != nil {
		C.free(unsafe.Pointer(ef.opaque))
		ef.opaque = nil
	}
}

//export frReadGo
func frReadGo(opaque_ unsafe.Pointer, buffer unsafe.Pointer, n C.size_t) C.uint64_t {
	opaque := (*C.fr_opaque)(opaque_)
	id := int64(opaque.id)

	p, ok := fileReaders.Load(id)
	if !ok {
		return 0
	}
	fr, ok := (p).(*FileReader)
	if !ok {
		return 0
	}

	size := int64(n)
	if fr.offset+size > fr.size {
		size = fr.size - fr.offset
	}
	if size <= 0 {
		return 0
	}

	buf := unsafe.Slice((*byte)(buffer), size)

	readBytes, err := fr.reader.ReadAt(buf, fr.offset)
	fr.offset += int64(readBytes)
	if err != nil && err != io.EOF {
		fr.err = err
		return C.uint64_t(readBytes)
	}

	return C.uint64_t(readBytes)
}

//export frSeekGo
func frSeekGo(opaque_ unsafe.Pointer, offset C.int64_t, origin C.int) C.bool {
	opaque := (*C.fr_opaque)(opaque_)
	id := int64(opaque.id)

	p, ok := fileReaders.Load(id)
	if !ok {
		return C.bool(false)
	}
	fr, ok := (p).(*FileReader)
	if !ok {
		return C.bool(false)
	}

	var whence int
	switch origin {
	case C.int(C.DMC_UNRAR_SEEK_SET):
		whence = io.SeekStart
	case C.int(C.DMC_UNRAR_SEEK_CUR):
		whence = io.SeekCurrent
	case C.int(C.DMC_UNRAR_SEEK_END):
		whence = io.SeekEnd
	default:
		return C.bool(false)
	}

	if _, err := fr.Seek(int64(offset), whence); err != nil {
		fr.err = err
		return C.bool(false)
	}
	return C.bool(true)
}

//export frTellGo
func frTellGo(opaque_ unsafe.Pointer) C.int64_t {
	opaque := (*C.fr_opaque)(opaque_)
	id := int64(opaque.id)

	p, ok := fileReaders.Load(id)
	if !ok {
		return C.int64_t(-1)
	}
	fr, ok := (p).(*FileReader)
	if !ok {
		return C.int64_t(-1)
	}
	return C.int64_t(fr.offset)
}

//export efCallbackGo
func efCallbackGo(opaque_ unsafe.Pointer, bufPtrPtr unsafe.Pointer, bufferSize *C.uint64_t, uncompressedSize C.uint64_t, ret *C.dmc_unrar_return) C.bool {
	_ = bufferSize // *buffer and *buffer_size are intentionally left unchanged; caller owns the buffer.

	opaque := (*C.ef_opaque)(opaque_)
	id := int64(opaque.id)

	p, ok := extractedFiles.Load(id)
	if !ok {
		*ret = C.DMC_UNRAR_WRITE_FAIL
		return C.bool(false)
	}
	ef, ok := (p).(*ExtractedFile)
	if !ok {
		*ret = C.DMC_UNRAR_WRITE_FAIL
		return C.bool(false)
	}

	size := int(uncompressedSize)
	if size == 0 {
		return C.bool(true)
	}

	bufPtr := *(*unsafe.Pointer)(bufPtrPtr)
	if bufPtr == nil {
		return C.bool(true)
	}
	buf := unsafe.Slice((*byte)(bufPtr), size)

	n, err := ef.writer.Write(buf)
	if err != nil {
		ef.err = err
		*ret = C.DMC_UNRAR_WRITE_FAIL
		return C.bool(false)
	}
	if n < size {
		ef.err = io.ErrShortWrite
		*ret = C.DMC_UNRAR_WRITE_FAIL
		return C.bool(false)
	}
	return C.bool(true)
}

//export cancelGo
func cancelGo(opaque_ unsafe.Pointer) C.bool {
	opaque := (*C.cancel_opaque)(opaque_)
	id := int64(opaque.id)

	p, ok := cancelStates.Load(id)
	if !ok {
		return C.bool(true)
	}
	cs, ok := (p).(*cancelState)
	if !ok {
		return C.bool(true)
	}

	ctx := cs.getCtx()
	if ctx == nil {
		return C.bool(true)
	}
	select {
	case <-ctx.Done():
		return C.bool(false)
	default:
		return C.bool(true)
	}
}

func checkError(name string, code C.dmc_unrar_return) error {
	if code == C.DMC_UNRAR_OK {
		return nil
	}

	str := C.dmc_unrar_strerror(code)
	return &Error{
		Operation: name,
		Code:      ErrorCode(code),
		Message:   C.GoString(str),
	}
}
