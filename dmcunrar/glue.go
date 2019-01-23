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
	"os"
	"reflect"
	"unsafe"

	"github.com/pkg/errors"
)

type FileReader struct {
	id     int64
	reader io.ReaderAt
	offset int64
	size   int64
	opaque *C.fr_opaque
	err    error
}

type Archive struct {
	fr      *FileReader
	archive *C.dmc_unrar_archive
}

func OpenArchiveFromPath(name string) (*Archive, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	stats, err := f.Stat()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	size := stats.Size()
	return OpenArchive(f, size)
}

func OpenArchive(reader io.ReaderAt, size int64) (*Archive, error) {
	fr := NewFileReader(reader, size)
	success := false
	defer func() {
		if !success {
			fr.Free()
		}
	}()

	a, err := openArchiveInternal(fr)
	if err != nil {
		return nil, err
	}

	success = true
	return a, err
}

func openArchiveInternal(fr *FileReader) (*Archive, error) {
	archive := (*C.dmc_unrar_archive)(C.malloc(C.sizeof_dmc_unrar_archive))
	success := false
	defer func() {
		if !success {
			C.free(unsafe.Pointer(archive))
		}
	}()

	var err error

	err = checkError("dmc_unrar_archive_init", C.dmc_unrar_archive_init(archive))
	if err != nil {
		return nil, err
	}

	archive.io.func_read = (C.dmc_unrar_read_func)(unsafe.Pointer(C.frReadGo_cgo))
	archive.io.func_seek = (C.dmc_unrar_seek_func)(unsafe.Pointer(C.frSeekGo_cgo))
	archive.io.opaque = unsafe.Pointer(fr.opaque)

	err = checkError("dmc_unrar_archive_open", C.dmc_unrar_archive_open(archive, C.uint64_t(fr.size)))
	if err != nil {
		return nil, err
	}

	a := &Archive{
		fr:      fr,
		archive: archive,
	}

	success = true
	return a, nil
}

func (a *Archive) Free() {
	if a.fr != nil {
		a.fr.Free()
		a.fr = nil
	}
}

func (a *Archive) GetFileCount() int64 {
	return int64(C.dmc_unrar_get_file_count(a.archive))
}

func (a *Archive) GetFilename(i int64) (string, error) {
	size := C.dmc_unrar_get_filename(
		a.archive,
		C.size_t(i),
		(*C.char)(nil),
		0,
	)
	if size == 0 {
		return "", errors.Errorf("0-length filename for entry %d", i)
	}

	filename := (*C.char)(C.malloc(size))
	defer C.free(unsafe.Pointer(filename))
	size = C.dmc_unrar_get_filename(
		a.archive,
		C.size_t(i),
		filename,
		size,
	)
	if size == 0 {
		return "", errors.Errorf("0-length filename for entry %d", i)
	}

	C.dmc_unrar_unicode_make_valid_utf8(filename)
	if *filename == 0 {
		return "", errors.Errorf("0-length filename (after make_valid_utf8) for entry %d", i)
	}

	return C.GoString(filename), nil
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
