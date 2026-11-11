package lockwatch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// lines as gdbus monitor prints them
func TestLockLine(t *testing.T) {
	cases := map[string]bool{
		"/org/freedesktop/login1/session/_32: org.freedesktop.login1.Session.Lock ()":                                                                                true,
		"/org/freedesktop/login1/session/_32: org.freedesktop.login1.Session.Unlock ()":                                                                              false,
		"/org/freedesktop/login1/session/_32: org.freedesktop.DBus.Properties.PropertiesChanged ('org.freedesktop.login1.Session', {'LockedHint': <true>}, @as [])":  true,
		"/org/freedesktop/login1/session/_32: org.freedesktop.DBus.Properties.PropertiesChanged ('org.freedesktop.login1.Session', {'LockedHint': <false>}, @as [])": false,
		"/org/freedesktop/login1: org.freedesktop.login1.Manager.PrepareForSleep (true,)":                                                                            true,
		"/org/freedesktop/login1: org.freedesktop.login1.Manager.PrepareForSleep (false,)":                                                                           false,
		"/org/freedesktop/ScreenSaver: org.freedesktop.ScreenSaver.ActiveChanged (true,)":                                                                            true,
		"/org/gnome/ScreenSaver: org.gnome.ScreenSaver.ActiveChanged (true,)":                                                                                        true,
		"/org/gnome/ScreenSaver: org.gnome.ScreenSaver.ActiveChanged (false,)":                                                                                       false,
		"/org/freedesktop/login1: org.freedesktop.login1.Manager.SessionNew ('33', objectpath '/org/freedesktop/login1/session/_33')":                                false,
		"The name org.freedesktop.login1 is owned by :1.4":                                                                                                           false,
	}
	for line, want := range cases {
		require.Equal(t, want, lockLine(line), line)
	}
}
