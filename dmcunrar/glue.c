#include <stdlib.h>
#include <stdint.h>
#include "_cgo_export.h"

size_t frReadGo_cgo(void *opaque, void *buffer, size_t n) {
	return frReadGo(opaque, buffer, n);
}

int frSeekGo_cgo(void *opaque, uint64_t offset) {
	return frSeekGo(opaque, offset);
}
