package webscenario

import (
	"context"
	"fmt"
	"time"

	"github.com/yuin/gopher-lua"
)

func RegisterTime(ctx context.Context, env *Environment) {
	env.RegisterNewType("time", map[string]lua.LGFunction{
		"now": func(L *lua.LState) int {
			env.Yield()
			L.Push(lua.LNumber(time.Now().UnixMilli()))
			return 1
		},
		"sleep": func(L *lua.LState) int {
			n := float64(L.CheckNumber(1))
			env.RecordOnAllTabs(L, fmt.Sprintf("time.sleep(%f)", n))

			dur := time.Duration(n * float64(time.Millisecond))
			AsyncRun(env, L, func() (struct{}, error) {
				var err error
				timer := time.NewTimer(dur)
				select {
				case <-timer.C:
					err = nil
				case <-ctx.Done():
					err = ctx.Err()
				}
				timer.Stop()
				return struct{}{}, err
			})

			env.RecordOnAllTabs(L, fmt.Sprintf("time.sleep(%f)", n))
			return 0
		},
		"format": func(L *lua.LState) int {
			env.Yield()

			n := L.CheckNumber(1)
			format := L.OptString(2, "%Y-%m-%dT%H:%M:%S%z")

			L.Push(L.GetField(L.GetGlobal("os"), "date"))
			L.Push(lua.LString(format))
			L.Push(lua.LNumber(n / 1000))
			L.Call(2, 1)
			return 1
		},
	}, map[string]lua.LValue{
		"millisecond": lua.LNumber(1),
		"second":      lua.LNumber(1000),
		"minute":      lua.LNumber(1000 * 60),
		"hour":        lua.LNumber(1000 * 60 * 60),
		"day":         lua.LNumber(1000 * 60 * 60 * 24),
		"week":        lua.LNumber(1000 * 60 * 60 * 24 * 7),
		"year":        lua.LNumber(1000 * 60 * 60 * 24 * 7 * 365),
	})
}
