//go:build js && wasm

package main

import (
	"github.com/Kirawii/qianben/backend/internal/ondevice"
	"syscall/js"
)

func main() {
	invoke := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) != 2 {
			return `{"status":422,"error":"需要本机状态和操作"}`
		}
		return ondevice.Invoke(args[0].String(), args[1].String())
	})
	js.Global().Set("qianbenInvoke", invoke)
	select {}
}
