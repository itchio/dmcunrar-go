package dmcunrar

import "C"

import (
	"sync"
	"sync/atomic"
)

var seed int64 = 1

//==============================
// FileReader
//==============================

var fileReaders sync.Map

func reserveFrId(obj *FileReader) {
	obj.id = atomic.AddInt64(&seed, 1)
	obj.opaque.id = C.long(obj.id)
	fileReaders.Store(obj.id, obj)
}

func freeFrId(id int64) {
	fileReaders.Delete(id)
}
