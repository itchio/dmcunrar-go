package dmcunrar

/*
#include <stdlib.h>
#include "dmc_unrar.h"

// gateway functions
size_t frReadGo_cgo(void *opaque, void *buffer, size_t n);
int frSeekGo_cgo(void *opaque, uint64_t offset);

typedef struct fr_opaque_tag {
	int64_t id;
} fr_opaque;
*/
import "C"

import (
	"io"
	"log"
	"os"
	"reflect"
	"unsafe"

	"github.com/pkg/errors"
)

type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

type FileReader struct {
	id     int64
	reader ReaderAtCloser
	offset int64
	size   int64
	opaque *C.fr_opaque
	io     *C.dmc_unrar_io
	err    error
}

type Archive struct {
	archive *C.dmc_unrar_archive
}

func Demo(file string) error {
	var archive C.dmc_unrar_archive

	var err error

	err = checkError("dmc_unrar_archive_init", C.dmc_unrar_archive_init(&archive))
	if err != nil {
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		return errors.WithStack(err)
	}
	stats, err := f.Stat()
	if err != nil {
		return errors.WithStack(err)
	}

	fr, err := NewFileReader(f, stats.Size())
	if err != nil {
		return errors.WithStack(err)
	}

	archive.io = *fr.io

	err = checkError("dmc_unrar_archive_open", C.dmc_unrar_archive_open(&archive, C.uint64_t(stats.Size())))
	if err != nil {
		return err
	}

	fileCount := int64(C.dmc_unrar_get_file_count(&archive))
	log.Printf("File count: %d", fileCount)

	for i := int64(0); i < fileCount; i++ {
		size := C.dmc_unrar_get_filename(
			&archive,
			C.size_t(i),
			(*C.char)(nil),
			0,
		)

		filename := (*C.char)(C.malloc(size))
		C.dmc_unrar_get_filename(
			&archive,
			C.size_t(i),
			filename,
			size,
		)

		name := C.GoString(filename)
		C.free(unsafe.Pointer(filename))
		log.Printf("%d: %s", i, name)
	}

	return nil
}

func NewFileReader(reader ReaderAtCloser, size int64) (*FileReader, error) {
	io := (*C.dmc_unrar_io)(C.malloc(C.sizeof_dmc_unrar_io))
	opaque := (*C.fr_opaque)(C.malloc(C.sizeof_fr_opaque))

	fr := &FileReader{
		reader: reader,
		offset: 0,
		size:   size,
		opaque: opaque,
		io:     io,
	}
	reserveFrId(fr)

	io.func_read = (C.dmc_unrar_read_func)(unsafe.Pointer(C.frReadGo_cgo))
	io.func_seek = (C.dmc_unrar_seek_func)(unsafe.Pointer(C.frSeekGo_cgo))
	io.opaque = unsafe.Pointer(opaque)

	return fr, nil
}

func (fr *FileReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		fr.offset = offset
	case io.SeekCurrent:
		fr.offset += offset
	case io.SeekEnd:
		fr.offset = fr.size + offset
	}

	return fr.offset, nil
}

//export frReadGo
func frReadGo(opaque_ unsafe.Pointer, buffer unsafe.Pointer, n C.size_t) C.size_t {
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

	h := reflect.SliceHeader{
		Data: uintptr(buffer),
		Cap:  int(size),
		Len:  int(size),
	}
	buf := *(*[]byte)(unsafe.Pointer(&h))

	readBytes, err := fr.reader.ReadAt(buf, fr.offset)
	fr.offset += int64(readBytes)
	if err != nil {
		fr.err = err
		return 0
	}

	return C.size_t(readBytes)
}

//export frSeekGo
func frSeekGo(opaque_ unsafe.Pointer, offset C.uint64_t) C.int {
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

	_, err := fr.Seek(int64(offset), io.SeekStart)
	if err != nil {
		fr.err = err
		return -1
	}

	return 0
}

func checkError(name string, code C.dmc_unrar_return) error {
	if code == C.DMC_UNRAR_OK {
		return nil
	}

	str := C.dmc_unrar_strerror(code)
	return errors.Errorf("%s: error %d: %s", name, code, C.GoString(str))
}
