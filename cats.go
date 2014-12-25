package libcats

import "fmt"

func GetCats(name string) string {
	return fmt.Sprintf("Meow, I'm %s!\n", name)
}
