package main

import "ozon/internal/app"

func main() {
	container, err := app.NewContainer()
	if err != nil {
		panic(err)
	}
	if err := container.Run(); err != nil {
		panic(err)
	}
}
