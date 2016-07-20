package pub_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/influx6/faux/pub"
)

// succeedMark is the Unicode codepoint for a check mark.
const succeedMark = "\u2713"

// failedMark is the Unicode codepoint for an X mark.
const failedMark = "\u2717"

// TestMousePosition provides a example test of a pub.Ctx that process mouse position (a slice of two values)
func TestMousePosition(t *testing.T) {
	var count int64

	pos := pub.Sync(func(r pub.Ctx, err error, data interface{}) {
		if err != nil {
			r.RW().Write(r, err)
			return
		}

		r.RW().Write(r, data)
		atomic.AddInt64(&count, 1)
	})

	for i := 0; i < 2000; i++ {
		pos.Read([]int{i * 3, i + 1})
	}

	if atomic.LoadInt64(&count) != 2000 {
		fatalFailed(t, "Total processed values is not equal, expected %d but got %d", 3000, count)
	}

	logPassed(t, "Total mouse data was processed with count %d", count)
}

// BenchmarkNodes benches the performance of using the Node api.
func BenchmarkNodes(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	dude := pub.DSync(func(r pub.Ctx, data interface{}) {
		r.RW().Write(r, data)
	})

	dudette := pub.DASync(func(r pub.Ctx, data interface{}) {
		time.Sleep(1 * time.Millisecond)
		r.RW().Write(r, data)
	})

	dude.Signal(dudette)

	for i := 0; i < b.N; i++ {
		dude.Read(i)
	}
}

func logPassed(t *testing.T, msg string, data ...interface{}) {
	t.Logf("%s %s", fmt.Sprintf(msg, data...), succeedMark)
}

func fatalFailed(t *testing.T, msg string, data ...interface{}) {
	t.Errorf("%s %s", fmt.Sprintf(msg, data...), failedMark)
}
