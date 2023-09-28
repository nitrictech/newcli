package project_new_test

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nitrictech/cli/pkg/operations/project_new"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"
)

var _ = Describe("ProjectNew", func() {
	var fs afero.Fs

	BeforeEach(func() {
		fs = afero.NewMemMapFs()
	})

	Describe("Non-Interactive", func() {
		Context("When provided with invalid Args", func() {
			model := project_new.New(
				fs,
				project_new.Args{
					ProjectName:  "InvalidProject",
					TemplateName: "InvalidTemplate",
				},
			)

			It("Should quit", func() {
				Expect(model.Init()()).To(Equal(tea.QuitMsg{}))
			})

			It("Should display an error", func() {
				Expect(model.View()).To(ContainSubstring("template \"InvalidTemplate\" could not be found"))
			})
		})
	})
})
