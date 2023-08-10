package main

import "fmt"

func main() {
	fmt.Println("")
	slice := make([]int, 0, 3)
	slice = append(slice, 1)
	slice = append(slice, 2)
	slice = append(slice, 3)
	slice = append(slice, 4, 5)
	slice = slice[2:]
	slice2 := []int{0, 0}
	copy(slice2, slice)
	fmt.Println(slice, len(slice), cap(slice))
	fmt.Println(slice2, len(slice2), cap(slice2))
}
