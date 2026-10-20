//go:build !darwin || ios || !cgo

package main

func preventAppNap() func() { return func() {} }
