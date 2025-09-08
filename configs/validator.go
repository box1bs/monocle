package configs

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
)

type validator struct {
	key string
}

func New(key string) *validator {
	return &validator{key: key}
}

func (v *validator) Validate(i any) error {
	fields := make([]reflect.StructField, 0)
	val := reflect.TypeOf(i)

	for i := range val.NumField() {
		fields = append(fields, val.Field(i))
	}

	for _, field := range fields {
		if field.Tag.Get(v.key) == "" {
			continue
		}

		f := reflect.ValueOf(i).FieldByName(field.Name)

		tagSTR := field.Tag.Get(v.key)
		tags := strings.SplitSeq(tagSTR, ",")

		for tag := range tags {
			entity := strings.SplitN(tag, "=", 2)

			switch strings.ToLower(entity[0]) {
			case "required":
				if f.Kind() != reflect.Bool && f.IsZero() {
					return errors.New("required field is empty: " + field.Name)
				}

			case "min":
				if f.Kind() == reflect.Int {
					border, err := strconv.ParseInt(entity[1], 10, 64)
					if err != nil {
						return err
					}
					if f.Int() < border {
						return errors.New("field " + field.Name + " less than min")
					}
				}

			case "max":
				if f.Kind() == reflect.Int {
					border, err := strconv.ParseInt(entity[1], 10, 64)
					if err != nil {
						return err
					}
					if f.Int() > border {
						return errors.New("field " + field.Name + " greater than max")
					}
				}

			case "len":
				if f.Kind() == reflect.Slice || f.Kind() == reflect.Array || f.Kind() == reflect.Map || f.Kind() == reflect.String {
					borders := strings.Split(entity[1], ":")
					if len(borders) == 2 {
						min, err := strconv.Atoi(borders[0])
						if err != nil {
							return err
						}
						max, err := strconv.Atoi(borders[1])
						if err != nil {
							return err
						}
						if f.Len() < min || f.Len() > max {
							return errors.New("field " + field.Name + " length not in range")
						}
					} else {
						return errors.New("invalid len tag format in field: " + field.Name)
					}
				}

			default:
				return errors.New("unknown tag: " + entity[0] + " in field: " + field.Name)
			}
		}
	}
	return nil
}