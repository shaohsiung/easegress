package main

import (
	"fmt"
	"reflect"
	"time"
)

type Filter interface {
	Kind() string
}

type FilterImpl1 struct {
	value string
}

func (f *FilterImpl1) Kind() string {
	return fmt.Sprintf("impl1 kind: %v", f.value)
}

type FilterImpl2 struct {
	value string
}

func (f *FilterImpl2) Kind() string {
	return fmt.Sprintf("impl2 kind: %v", f.value)
}

func main() {
	//reflectTypeof()
	closeChan()
}

func reflectTypeof() {
	f1 := &FilterImpl1{}
	filterType := reflect.TypeOf(f1)
	fmt.Printf("%s\n", filterType)                                 // *main.FilterImpl1
	fmt.Printf("%v\n", filterType.Kind() == reflect.Ptr)           // true
	fmt.Printf("%v\n", filterType.Elem())                          // main.FilterImpl1
	fmt.Printf("%v\n", filterType.Elem().Kind() == reflect.Struct) // true
}

func closeChan() {
	ch := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				time.Sleep(2 * time.Second)
				fmt.Println("done")
				return
			}
		}
	}()

	time.Sleep(1 * time.Second)

	close(ch)
	fmt.Println("ch closed")
	time.Sleep(3 * time.Second)
}
