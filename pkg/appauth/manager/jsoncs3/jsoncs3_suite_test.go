package jsoncs3_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJsoncs3(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Jsoncs3 Suite")
}
