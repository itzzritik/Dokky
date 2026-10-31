package engine

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs/config/obscure"
)

type memConfig struct {
	mu      sync.RWMutex
	data    map[string]map[string]string
	onToken func(serviceID, token string)
}

func newMemConfig(onToken func(serviceID, token string)) *memConfig {
	return &memConfig{data: map[string]map[string]string{}, onToken: onToken}
}

func (m *memConfig) PutService(s store.Service, isPassword func(typ, key string) bool) {
	sec := map[string]string{}
	for k, v := range s.Options {
		sec[k] = v
	}
	for k, v := range s.Secrets {
		sec[k] = v
	}
	for k, v := range sec {
		if isPassword(s.Type, k) {
			sec[k] = obscure.MustObscure(v)
		}
	}
	sec["type"] = s.Type
	m.mu.Lock()
	m.data[s.ID] = sec
	m.mu.Unlock()
}

func (m *memConfig) DeleteService(id string) { m.DeleteSection(id) }

func (m *memConfig) GetSectionList() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Sorted(maps.Keys(m.data))
}

func (m *memConfig) HasSection(section string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[section]
	return ok
}

func (m *memConfig) DeleteSection(section string) {
	m.mu.Lock()
	delete(m.data, section)
	m.mu.Unlock()
}

func (m *memConfig) GetKeyList(section string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Sorted(maps.Keys(m.data[section]))
}

func (m *memConfig) GetValue(section, key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[section][key]
	return v, ok
}

func (m *memConfig) SetValue(section, key, value string) {
	m.mu.Lock()
	sec := m.data[section]
	if sec == nil {
		sec = map[string]string{}
		m.data[section] = sec
	}
	old, had := sec[key]
	sec[key] = value
	m.mu.Unlock()
	if key == "token" && (!had || old != value) && m.onToken != nil {
		m.onToken(section, value)
	}
}

func (m *memConfig) DeleteKey(section, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[section][key]
	delete(m.data[section], key)
	return ok
}

func (m *memConfig) Load() error { return nil }

func (m *memConfig) Save() error { return nil }

func (m *memConfig) Serialize() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(m.data)) {
		fmt.Fprintf(&b, "[%s]\n", name)
		for _, k := range slices.Sorted(maps.Keys(m.data[name])) {
			fmt.Fprintf(&b, "%s = %s\n", k, m.data[name][k])
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
