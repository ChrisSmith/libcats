// +build android,cgo

package libcats

/*

#include <pthread.h>
#include <stdlib.h>

long threadID() {
	long id;
	id = gettid();
	return id;
}

*/
import "C"
import "fmt"

func GetThreadId() int64 {
	return int64(C.threadID())
}

func SetThreadLogger() {
	loggerFunc = func(format string, args ...interface{}) {
		fmt.Printf("thread:[%d] %s\n", GetThreadId(), fmt.Sprintf(format, args...))
	}
}
