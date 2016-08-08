package tagconfig_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"sync"

	"errors"

	. "github.com/Experticity/tagconfig"
	"github.com/stretchr/testify/assert"
)

type Specification struct {
	SystemsCrashCount int    `emaNgaT:"crash.count" required:"true"`
	Handle            string `emaNgaT:"handle" default:"zero.cool"`
}

type SpecificationWithEmbedded struct {
	Relationship string `emaNgaT:"relation.ship" default:"parent"`
	EmbeddedSpecification
	Ignored `ignored:"true"`
}

type EmbeddedSpecification struct {
	Relationship string `emaNgaT:"embed.relation.ship"`
}

type Ignored struct {
	Name string `emaNgaT:"name" default:"nombre"`
}

type MockGetter struct {
	Values map[string]string
}

func (mg *MockGetter) Get(key string, t reflect.StructField) string {
	return mg.Values[key]
}

func (mg *MockGetter) TagName() string {
	return "emaNgaT"
}

type mockSetter struct {
	sync.Mutex
	mem map[string]string
}

func (m *mockSetter) TagName() string {
	return "bl"
}

func (m *mockSetter) Set(key string, value interface{}, _ reflect.StructField) error {
	v, ok := value.(string)
	if !ok {
		return errors.New("Received a non string")
	}

	if v != "" {
		m.Lock()
		defer m.Unlock()
		m.mem[key] = v
	}
	return nil
}

func TestValidSpec(t *testing.T) {
	spec := &Specification{}

	mg := &MockGetter{Values: make(map[string]string)}

	crashCount := "1507"
	handle := "crash.override"
	mg.Values["crash.count"] = crashCount
	mg.Values["handle"] = handle

	err := Process(mg, spec)

	assert.NoError(t, err)

	port, _ := strconv.Atoi(crashCount)

	assert.Equal(t, port, spec.SystemsCrashCount)
	assert.Equal(t, handle, spec.Handle)
}

func TestMissingRequired(t *testing.T) {
	spec := &Specification{}

	mg := &MockGetter{Values: make(map[string]string)}

	err := Process(mg, spec)

	assert.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "required key"))
}

func TestDefaultValue(t *testing.T) {
	spec := &Specification{}

	mg := &MockGetter{Values: make(map[string]string)}

	mg.Values["crash.count"] = "1507"

	err := Process(mg, spec)

	assert.NoError(t, err)
	assert.Equal(t, spec.Handle, "zero.cool")
}

func TestEmbedded(t *testing.T) {
	spec := &SpecificationWithEmbedded{}

	mg := &MockGetter{Values: make(map[string]string)}
	mg.Values["embed.relation.ship"] = "child"

	err := Process(mg, spec)

	assert.NoError(t, err)
	assert.Equal(t, spec.EmbeddedSpecification.Relationship, "child")
}

func TestEmbeddedButIgnored(t *testing.T) {
	spec := &SpecificationWithEmbedded{}

	mg := &MockGetter{Values: make(map[string]string)}

	err := Process(mg, spec)

	assert.NoError(t, err)
	assert.Equal(t, spec.Ignored.Name, "")

	err = Process(mg, &spec.Ignored)

	assert.NoError(t, err)
	assert.Equal(t, spec.Ignored.Name, "nombre")
}

func TestPopulateExternalSourceSuccessful(t *testing.T) {
	type (
		Meta struct {
			Activity string `bl:"meta.activity"`
			Item     string `bl:"meta.item"`
			Animal   string
		}

		person struct {
			Name string `bl:"name"`
			Age  string `bl:"age"`
			Meta
		}
	)

	tests := []struct {
		p       *person
		expFunc func(*person) map[string]string
	}{
		{
			&person{
				Name: "The dude",
				Age:  "40",
			},
			func(p *person) map[string]string {
				return map[string]string{
					"name": p.Name,
					"age":  p.Age,
				}
			},
		},

		{
			&person{
				Name: "The dude",
				Age:  "40",
				Meta: Meta{
					Animal: "marmot",
				},
			},
			func(p *person) map[string]string {
				return map[string]string{
					"name": p.Name,
					"age":  p.Age,
				}
			},
		},

		{
			&person{
				Name: "The dude",
				Age:  "40",
				Meta: Meta{
					Activity: "bowling",
					Item:     "rug",
				},
			},
			func(p *person) map[string]string {
				return map[string]string{
					"name":          p.Name,
					"age":           p.Age,
					"meta.activity": p.Activity,
					"meta.item":     p.Item,
				}
			},
		},
	}

	for _, tt := range tests {
		m := &mockSetter{
			mem: map[string]string{},
		}

		err := PopulateExternalSource(m, tt.p)
		assert.NoError(t, err)
		assert.Equal(t, tt.expFunc(tt.p), m.mem)
	}
}

func TestPopulateExternalSourceError(t *testing.T) {
	tests := []struct {
		valFunc func() interface{}
	}{
		{
			func() interface{} {
				type person struct {
					Name   string `bl:"name"`
					Abides bool   `bl:"abides"`
				}
				return &person{"His Dudeness", true}
			},
		},

		{
			func() interface{} {
				type person struct {
					Name string `bl:"name"`
				}
				return person{"His Dudeness"}
			},
		},

		{
			func() interface{} {
				return "what do you think this is?"
			},
		},
	}

	for _, tt := range tests {
		m := &mockSetter{
			mem: map[string]string{},
		}

		err := PopulateExternalSource(m, tt.valFunc())
		assert.Error(t, err)
	}
}
