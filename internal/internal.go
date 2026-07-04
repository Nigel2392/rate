package internal

import "reflect"

type isZeroer interface {
	IsZero() bool
}

var _isZeroerType = reflect.TypeOf((*isZeroer)(nil)).Elem()

func IsZero(value interface{}) bool {
	var rv reflect.Value
	switch v := value.(type) {
	case reflect.Value:
		rv = v
	case *reflect.Value:
		if v == nil {
			return true
		}
		rv = *v
	default:
		rv = reflect.ValueOf(value)
	}

	if !rv.IsValid() {
		return true
	}

	if rv.Kind() == reflect.Interface && rv.IsNil() {
		return true
	}

	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return true
	}

	// check if either the pointer to the value or the value itself implements isZeroer
	if rv.Type().Implements(_isZeroerType) {
		return rv.Interface().(isZeroer).IsZero()
	} else if rv.Kind() == reflect.Ptr && !rv.IsNil() && rv.Elem().Type().Implements(_isZeroerType) {
		return rv.Elem().Interface().(isZeroer).IsZero()
	}

	switch rv.Kind() {
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Complex64, reflect.Complex128:
		return rv.Complex() == 0
	case reflect.Ptr:
		if !rv.IsValid() || rv.IsNil() {
			return true
		}
		return IsZero(rv.Elem().Interface())
	case reflect.String:
		return rv.String() == ""
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return true
		}

		for i := 0; i < rv.Len(); i++ {
			if !IsZero(rv.Index(i).Interface()) {
				return false
			}
		}
	case reflect.Map:
		return rv.Len() == 0
	}

	return reflect.DeepEqual(rv.Interface(), reflect.Zero(rv.Type()).Interface())
}
