package promise

import (
	"syscall/js"
)

func New(fn func(resolve, reject js.Value)) js.Value {
	promiseConstructor := js.Global().Get("Promise")
	// Create and return the Promise object
	return promiseConstructor.New(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]
		reject := args[1]
		fn(resolve, reject)
		// The handler of a Promise doesn't return any value
		return nil
	}))
}
