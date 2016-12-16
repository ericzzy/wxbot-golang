package main

import (
	"fmt"
	"math/rand"
	"strconv"
)

func main() {
	fmt.Println("e" + (strconv.FormatFloat(rand.Float64(), 'f', -1, 64))[2:17])
}
