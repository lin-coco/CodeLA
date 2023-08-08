package main

import "fmt"

func main() {
	channel := make(chan struct{}, 1)
	go func() {
		channel <- struct{}{}
	}()
	fmt.Println(<-channel)
	close(channel)
}
