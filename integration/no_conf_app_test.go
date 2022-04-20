package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/paketo-buildpacks/occam"
	"github.com/paketo-buildpacks/packit/v2/fs"

	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
	. "github.com/paketo-buildpacks/occam/matchers"
)

func testNoConfApp(t *testing.T, context spec.G, it spec.S) {
	var (
		Expect     = NewWithT(t).Expect
		Eventually = NewWithT(t).Eventually

		pack   occam.Pack
		docker occam.Docker

		name      string
		source    string
		image     occam.Image
		container occam.Container
	)

	it.Before(func() {
		pack = occam.NewPack().WithNoColor().WithVerbose()

		docker = occam.NewDocker()

		var err error
		name, err = occam.RandomName()
		Expect(err).NotTo(HaveOccurred())

		source, err = occam.Source(filepath.Join("testdata", "no_conf_app"))
		Expect(err).NotTo(HaveOccurred())
	})

	it.After(func() {
		Expect(docker.Container.Remove.Execute(container.ID)).To(Succeed())
		Expect(docker.Image.Remove.Execute(image.ID)).To(Succeed())
		Expect(docker.Volume.Remove.Execute(occam.CacheVolumeNames(name))).To(Succeed())
		Expect(os.RemoveAll(source)).To(Succeed())
	})

	context("when pushing app with no conf and $BP_WEB_SERVER=nginx", func() {
		it("generates the default nginx.conf", func() {
			var err error
			image, _, err = pack.Build.
				WithBuildpacks(nginxBuildpack).
				WithEnv(map[string]string{
					"BP_WEB_SERVER": "nginx",
				}).
				WithPullPolicy("never").
				Execute(name, source)
			Expect(err).NotTo(HaveOccurred())

			container, err = docker.Container.Run.
				WithEnv(map[string]string{"PORT": "8080"}).
				WithPublish("8080").
				Execute(image.ID)
			Expect(err).ToNot(HaveOccurred())

			Eventually(container).Should(Serve(ContainSubstring("<p>Hello World!</p>")).OnPort(8080))
		})
	})

	context("when using env var configuration options", func() {
		it.Before(func() {
			Expect(fs.Copy(filepath.Join(source, "public"), filepath.Join(source, "custom_root"))).To(Succeed())
			os.RemoveAll(filepath.Join(source, "public"))
		})
		it("generates an nginx.conf with the configuration", func() {
			var err error
			image, _, err = pack.Build.
				WithBuildpacks(nginxBuildpack).
				WithEnv(map[string]string{
					"BP_WEB_SERVER":                   "nginx",
					"BP_WEB_SERVER_ROOT":              "custom_root",
					"BP_WEB_SERVER_ENABLE_PUSH_STATE": "true",
				}).
				WithPullPolicy("never").
				Execute(name, source)
			Expect(err).NotTo(HaveOccurred())

			container, err = docker.Container.Run.
				WithEnv(map[string]string{"PORT": "8080"}).
				WithPublish("8080").
				Execute(image.ID)
			Expect(err).ToNot(HaveOccurred())

			Eventually(container).Should(Serve(ContainSubstring("<p>Hello World!</p>")).OnPort(8080))
			Eventually(container).Should(Serve(ContainSubstring("<p>Hello World!</p>")).OnPort(8080).WithEndpoint("/test"))
		})
	})

	context("building with no config and forcing HTTPS connections", func() {
		it("generates an nginx.conf with the required redirect logic", func() {
			var err error
			image, _, err = pack.Build.
				WithBuildpacks(nginxBuildpack).
				WithEnv(map[string]string{
					"BP_WEB_SERVER":             "nginx",
					"BP_WEB_SERVER_FORCE_HTTPS": "true",
				}).
				WithPullPolicy("never").
				Execute(name, source)
			Expect(err).NotTo(HaveOccurred())

			container, err = docker.Container.Run.
				WithEnv(map[string]string{"PORT": "8080"}).
				WithPublish("8080").
				Execute(image.ID)
			Expect(err).ToNot(HaveOccurred())

			client := &http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			response, err := client.Get(fmt.Sprintf("http://localhost:%s", container.HostPort("8080")))
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			Expect(response.StatusCode).To(Equal(http.StatusMovedPermanently))

			_, err = http.Get(fmt.Sprintf("http://localhost:%s", container.HostPort("8080")))
			// Assert that the server attempts to hit HTTPS URL instead of HTTP
			Expect(err).To(MatchError(`Get "https://localhost/": dial tcp [::1]:443: connect: connection refused`))
		})
	})

	context("when htpasswd service binding is provided", func() {
		it.Before(func() {
			var err error
			source, err = occam.Source(filepath.Join("testdata", "basic_auth_app"))
			Expect(err).NotTo(HaveOccurred())
		})

		it("password-protects the served static files", func() {
			var err error
			image, _, err = pack.Build.
				WithBuildpacks(nginxBuildpack).
				WithEnv(map[string]string{
					"BP_WEB_SERVER":        "nginx",
					"SERVICE_BINDING_ROOT": "/bindings",
				}).
				WithVolumes(fmt.Sprintf("%s:/bindings/auth", filepath.Join(source, "binding"))).
				WithPullPolicy("never").
				Execute(name, filepath.Join(source, "app"))
			Expect(err).NotTo(HaveOccurred())

			container, err = docker.Container.Run.
				WithEnv(map[string]string{"PORT": "8080"}).
				WithVolumes(fmt.Sprintf("%s:/bindings/auth", filepath.Join(source, "binding"))).
				WithPublish("8080").
				Execute(image.ID)
			Expect(err).ToNot(HaveOccurred())

			// Assert that unauthenticated requests fail
			response, err := http.Get(fmt.Sprintf("http://localhost:%s", container.HostPort("8080")))
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

			// And that authenticated requests succeed
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s", container.HostPort("8080")), http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			req.SetBasicAuth("user", "password")

			response, err = http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			contents, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contents)).To(ContainSubstring("Hello World!"))
		})
	})
}
