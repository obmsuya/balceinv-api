package validation

import (
	"errors"
	"reflect"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/go-playground/validator/v10"
)

var structValidator = newStructValidator()

func newStructValidator() *validator.Validate {
	createdValidator := validator.New(validator.WithRequiredStructEnabled())
	createdValidator.RegisterTagNameFunc(jsonFieldName)
	return createdValidator
}

func Validate(requestBody any) []response.FieldError {
	validationError := structValidator.Struct(requestBody)
	if validationError == nil {
		return nil
	}

	var fieldErrors validator.ValidationErrors
	isFieldErrorList := errors.As(validationError, &fieldErrors)
	if !isFieldErrorList {
		return []response.FieldError{
			{
				Field:   "",
				Message: "request body is invalid",
			},
		}
	}

	collectedErrors := make([]response.FieldError, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		collectedErrors = append(collectedErrors, response.FieldError{
			Field:   fieldError.Field(),
			Message: describeRule(fieldError),
		})
	}

	return collectedErrors
}

func jsonFieldName(structField reflect.StructField) string {
	jsonTag := structField.Tag.Get("json")
	fieldName, _, _ := strings.Cut(jsonTag, ",")
	if fieldName == "" || fieldName == "-" {
		return structField.Name
	}
	return fieldName
}

func describeRule(fieldError validator.FieldError) string {
	switch fieldError.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fieldError.Param() + " characters"
	case "max":
		return "must be at most " + fieldError.Param() + " characters"
	case "len":
		return "must be exactly " + fieldError.Param() + " characters"
	case "oneof":
		return "must be one of: " + fieldError.Param()
	case "uuid":
		return "must be a valid id"
	case "hexcolor":
		return "must be a colour like #1a2b3c"
	default:
		return "is invalid (" + fieldError.Tag() + ")"
	}
}
