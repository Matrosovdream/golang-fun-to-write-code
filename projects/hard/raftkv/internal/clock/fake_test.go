package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFakeTickerFiresOncePerPeriod(t *testing.T) {
	f := NewFake()
	tk := f.NewTicker(50 * time.Millisecond)
	defer tk.Stop()

	f.Advance(40 * time.Millisecond)
	require.Empty(t, tk.C())
	f.Advance(10 * time.Millisecond)
	require.Len(t, tk.C(), 1)
	<-tk.C()

	f.Advance(50 * time.Millisecond)
	require.Len(t, tk.C(), 1)
}

func TestFakeAfterFuncOrderAndStop(t *testing.T) {
	f := NewFake()
	var got []string
	f.AfterFunc(30*time.Millisecond, func() { got = append(got, "b") })
	f.AfterFunc(10*time.Millisecond, func() {
		got = append(got, "a")
		f.AfterFunc(10*time.Millisecond, func() { got = append(got, "a2") }) // due inside the same Advance
	})
	f.AfterFunc(time.Second, func() { got = append(got, "late") })

	f.Advance(100 * time.Millisecond)
	require.Equal(t, []string{"a", "a2", "b"}, got)

	tk := f.NewTicker(10 * time.Millisecond)
	tk.Stop()
	f.Advance(time.Second)
	require.Empty(t, tk.C())
}
