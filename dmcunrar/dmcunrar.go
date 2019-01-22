package dmcunrar

/*
#include <stdlib.h>
#include "dmc_unrar.h"
*/
import "C"

import (
	"log"
	"unsafe"

	"github.com/pkg/errors"
)

func Demo(file string) error {
	var archive C.dmc_unrar_archive
	var return_code C.dmc_unrar_return

	return_code = C.dmc_unrar_archive_init(&archive)
	if return_code != C.DMC_UNRAR_OK {
		return errors.Errorf("dmc_unrar_archive_init: code %d", return_code)
	}

	cFile := C.CString(file)
	defer C.free(unsafe.Pointer(cFile))

	return_code = C.dmc_unrar_archive_open_path(&archive, cFile)
	if return_code != C.DMC_UNRAR_OK {
		return errors.Errorf("dmc_unrar_archive_open_path: code %d", return_code)
	}

	fileCount := int64(C.dmc_unrar_get_file_count(&archive))
	log.Printf("File count: %d", fileCount)

	for i := int64(0); i < fileCount; i++ {
		size := C.dmc_unrar_get_filename(
			&archive,
			C.ulonglong(i),
			(*C.char)(nil),
			0,
		)

		filename := (*C.char)(C.malloc(size))
		C.dmc_unrar_get_filename(
			&archive,
			C.ulonglong(i),
			filename,
			size,
		)

		name := C.GoString(filename)
		C.free(unsafe.Pointer(filename))
		log.Printf("%d: %s", i, name)
	}

	return nil
}
