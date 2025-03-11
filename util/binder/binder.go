package binder

import (
	"sync"
	"sync/atomic"
	"syscall/js"
)

const idKey = "_go_obj_7788_"

var (
	seqID int32
	m     = &sync.Map{}
)

func Bind(jsValue js.Value, obj interface{}) {
	id := atomic.AddInt32(&seqID, 1)
	m.Store(id, obj)
	jsValue.Set(idKey, id)
}

func GetObject(jsValue js.Value) interface{} {
	id := int32(jsValue.Get(idKey).Int())
	v, _ := m.Load(int32(id))
	return v
}

func Unbind(jsValue js.Value) {
	id := int32(jsValue.Get(idKey).Int())
	m.Delete(id)
}
