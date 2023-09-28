package project_new_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProjectNew(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProjectNew Suite")
}
