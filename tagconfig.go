// MIT Licensed
package tagconfig

// 99% of this work comes from Kelsey Hightower's envconfig (https://github.com/kelseyhightower/envconfig/blob/master/envconfig.go)
// I take no credit for the process bits, all I've changed is adding some interfaces to allow for an implementation that would
// get environment variables, or remote key value store, or config file or or or.
import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidSpecification indicates that a specification is of the wrong type.
var ErrInvalidSpecification = errors.New("specification must be a struct pointer")

// A ParseError occurs when an environment variable cannot be converted to
// the type required by a struct field during assignment.
type ParseError struct {
	FieldName string
	TypeName  string
	Value     string
}

// A Decoder is a type that knows how to de-serialize environment variables
// into itself.
type Decoder interface {
	Decode(value string) error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("tagconfig.Process: assigning to %[1]s: converting '%[2]s' to type %[3]s", e.FieldName, e.Value, e.TypeName)
}

// TagNameGetter is used for defining a struct tag that contains the value in
// question.
type TagNameGetter interface {
	TagName() string
}

// TagValueGetter is an interface to allow for different k,v stores to be passed to the Process func.
// TagName is the name of the tag Process looks for, to query the TagValueGetter
// Get is passed a key to look up and the StructField so it can inspect for other tags it might care about.
type TagValueGetter interface {
	TagNameGetter
	Get(key string, t reflect.StructField) string
}

// TagValueSetter is used to set a value onto an external source based on the
// tag name.
type TagValueSetter interface {
	TagNameGetter
	Set(key string, value interface{}, t reflect.StructField) error
}

// Process populates the specified struct based on the TagValueGetter implementation
func Process(v TagValueGetter, spec interface{}) error {
	s := reflect.ValueOf(spec)

	if s.Kind() != reflect.Ptr {
		return ErrInvalidSpecification
	}
	s = s.Elem()
	if s.Kind() != reflect.Struct {
		return ErrInvalidSpecification
	}
	typeOfSpec := s.Type()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		if !f.CanSet() || typeOfSpec.Field(i).Tag.Get("ignored") == "true" {
			continue
		}

		if typeOfSpec.Field(i).Anonymous && f.Kind() == reflect.Struct {
			embeddedPtr := f.Addr().Interface()
			if err := Process(v, embeddedPtr); err != nil {
				return err
			}
			f.Set(reflect.ValueOf(embeddedPtr).Elem())
		}

		// Pull the key from TagValueGetter
		key := typeOfSpec.Field(i).Tag.Get(v.TagName())
		if key == "" {
			continue
		}

		// Let the TagValueGetter decide how to extract the value and pass
		// along the structField so it can inspect potential meta data.
		value := v.Get(key, typeOfSpec.Field(i))

		def := typeOfSpec.Field(i).Tag.Get("default")
		if def != "" && value == "" {
			value = def
		}

		req := typeOfSpec.Field(i).Tag.Get("required")
		if value == "" && def == "" {
			if req == "true" {
				return fmt.Errorf("required key %s missing value", key)
			}
			continue
		}

		err := processField(value, f)
		if err != nil {
			return &ParseError{
				FieldName: key,
				TypeName:  f.Type().String(),
				Value:     value,
			}
		}
	}
	return nil
}

// MustProcess is the same as Process but panics if an error occurs
func MustProcess(v TagValueGetter, spec interface{}) {
	if err := Process(v, spec); err != nil {
		panic(err)
	}
}

func processField(value string, field reflect.Value) error {
	typ := field.Type()

	decoder := decoderFrom(field)
	if decoder != nil {
		return decoder.Decode(value)
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		if field.IsNil() {
			field.Set(reflect.New(typ))
		}
		field = field.Elem()
	}

	switch typ.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var (
			val int64
			err error
		)
		if field.Kind() == reflect.Int64 && typ.PkgPath() == "time" && typ.Name() == "Duration" {
			var d time.Duration
			d, err = time.ParseDuration(value)
			val = int64(d)
		} else {
			val, err = strconv.ParseInt(value, 0, typ.Bits())
		}
		if err != nil {
			return err
		}

		field.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(value, 0, typ.Bits())
		if err != nil {
			return err
		}
		field.SetUint(val)
	case reflect.Bool:
		val, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(value, typ.Bits())
		if err != nil {
			return err
		}
		field.SetFloat(val)
	case reflect.Slice:
		vals := strings.Split(value, ",")
		sl := reflect.MakeSlice(typ, len(vals), len(vals))
		for i, val := range vals {
			err := processField(val, sl.Index(i))
			if err != nil {
				return err
			}
		}
		field.Set(sl)
	}

	return nil
}

func decoderFrom(field reflect.Value) Decoder {
	if field.CanInterface() {
		dec, ok := field.Interface().(Decoder)
		if ok {
			return dec
		}
	}

	// also check if pointer-to-type implements Decoder,
	// and we can get a pointer to our field
	if field.CanAddr() {
		field = field.Addr()
		dec, ok := field.Interface().(Decoder)
		if ok {
			return dec
		}
	}

	return nil
}

// PopulateExternalSource is used to fill an external source when a struct
// contains values. An external source can include an in memory store,
// environment variables, a properties store, and so forth. Therefore, if a
// struct contains values, but you want to propagate said values to another
// location, this would allow you to do so based off of how the TagValueSetter
// has been implemented.
func PopulateExternalSource(v TagValueSetter, spec interface{}) error {
	s := reflect.ValueOf(spec)

	if s.Kind() != reflect.Ptr {
		return ErrInvalidSpecification
	}

	s = s.Elem()
	if s.Kind() != reflect.Struct {
		return ErrInvalidSpecification
	}
	typeOfSpec := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		ft := typeOfSpec.Field(i)

		if typeOfSpec.Field(i).Anonymous && f.Kind() == reflect.Struct {
			embeddedPtr := f.Addr().Interface()
			if err := PopulateExternalSource(v, embeddedPtr); err != nil {
				return err
			}
		} else {
			t := ft.Tag.Get(v.TagName())
			if t != "" {
				err := v.Set(t, f.Interface(), ft)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
