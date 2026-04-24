#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include "_cgo_export.h"

static uint64_t fr_read(void *opaque, void *buffer, uint64_t n) {
	return frReadGo(opaque, buffer, (size_t)n);
}

static bool fr_seek(void *opaque, int64_t offset, int origin) {
	return frSeekGo(opaque, offset, origin);
}

static int64_t fr_tell(void *opaque) {
	return frTellGo(opaque);
}

static void *fr_open_noop(const char *path) {
	(void)path;
	return NULL;
}

static void fr_close_noop(void *opaque) {
	(void)opaque;
}

static dmc_unrar_io_handler dmc_unrar_go_handler = {
	fr_open_noop,
	fr_close_noop,
	fr_read,
	fr_seek,
	fr_tell,
};

dmc_unrar_io_handler *dmc_unrar_go_get_handler(void) {
	return &dmc_unrar_go_handler;
}

bool efCallbackGo_cgo(void *opaque, void **buffer, uint64_t *buffer_size,
                      uint64_t uncompressed_size, dmc_unrar_return *err) {
	return efCallbackGo(opaque, buffer, buffer_size, uncompressed_size, err);
}

bool cancelGo_cgo(void *opaque) {
	return cancelGo(opaque);
}

/* dmc_unrar_cancel.func is a reserved Go keyword, so cgo cannot assign it
   directly from Go. Set it from C instead. Pass NULL func/opaque to disable. */
void dmc_unrar_go_set_cancel(dmc_unrar_archive *archive,
                             dmc_unrar_cancel_func func, void *opaque) {
	archive->cancel.func = func;
	archive->cancel.opaque = opaque;
}
