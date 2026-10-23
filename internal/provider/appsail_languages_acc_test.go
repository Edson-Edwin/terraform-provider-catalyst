// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// All fixtures are packaged by Go. Python is a target deployment language only;
// no Python tool or Python helper script is executed to build or test the provider.
func writeAppSailLanguageArchive(t *testing.T, dir, stack, message string) string {
	t.Helper()
	files := map[string][]byte{}
	switch {
	case strings.HasPrefix(stack, "node"):
		return writeAppSailArchive(t, dir, "website", message)
	case strings.HasPrefix(stack, "python"):
		files["main.py"] = []byte("from http.server import BaseHTTPRequestHandler, HTTPServer\nimport os\nclass Handler(BaseHTTPRequestHandler):\n def do_GET(self):\n  self.send_response(200)\n  self.end_headers()\n  self.wfile.write(b'" + message + "')\nHTTPServer(('0.0.0.0', int(os.environ.get('X_ZOHO_CATALYST_LISTEN_PORT', '9000'))), Handler).serve_forever()\n")
	case strings.HasPrefix(stack, "java"):
		javaDir := t.TempDir()
		src := `import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
public class Website {public static void main(String[] args) throws Exception {
String port=System.getenv("X_ZOHO_CATALYST_LISTEN_PORT");
HttpServer server=HttpServer.create(new InetSocketAddress("0.0.0.0",port==null?9000:Integer.parseInt(port)),0);
server.createContext("/",exchange->{byte[] body="` + message + `".getBytes(StandardCharsets.UTF_8);exchange.sendResponseHeaders(200,body.length);exchange.getResponseBody().write(body);exchange.close();});server.start();}}
`
		source := filepath.Join(javaDir, "Website.java")
		if err := os.WriteFile(source, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
		// Java 8 bytecode runs on the newer managed Java runtimes as well.
		cmd := exec.CommandContext(t.Context(), "javac", "--release", "8", "-d", javaDir, source)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("compile generated Java fixture: %v: %s", err, output)
		}
		class, err := os.ReadFile(filepath.Join(javaDir, "Website.class"))
		if err != nil {
			t.Fatal(err)
		}
		var jar bytes.Buffer
		jw := zip.NewWriter(&jar)
		for name, content := range map[string][]byte{"META-INF/MANIFEST.MF": []byte("Manifest-Version: 1.0\r\nMain-Class: Website\r\n\r\n"), "Website.class": class} {
			f, err := jw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write(content); err != nil {
				t.Fatal(err)
			}
		}
		if err = jw.Close(); err != nil {
			t.Fatal(err)
		}
		files["website.jar"] = jar.Bytes()
	default:
		t.Fatal("unsupported fixture language")
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range files {
		f, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, stack+"-"+message+".zip")
	if err := os.WriteFile(target, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return target
}
func appSailLanguageConfig(project, name, source, stack string, updated bool) string {
	config := appsailConfig(project, name, source, updated)
	extra := fmt.Sprintf("\n stack = %q\n", stack)
	if strings.HasPrefix(stack, "java") {
		extra += " platform = \"javase\"\n"
		command := "java -jar website.jar"
		if updated {
			command = "java -Dfixture=true -jar website.jar"
		}
		config = strings.Replace(config, `startup_command = "node index.js"`, fmt.Sprintf("startup_command = %q", command), 1)
		config = strings.Replace(config, `startup_command = "node --no-warnings index.js"`, fmt.Sprintf("startup_command = %q", command), 1)
	}
	if strings.HasPrefix(stack, "python") {
		command := "python3 main.py"
		if updated {
			command = "python3 -u main.py"
		}
		config = strings.Replace(config, `startup_command = "node index.js"`, fmt.Sprintf("startup_command = %q", command), 1)
		config = strings.Replace(config, `startup_command = "node --no-warnings index.js"`, fmt.Sprintf("startup_command = %q", command), 1)
	}
	if updated {
		config = strings.Replace(config, `catalyst_auth = true`, `catalyst_auth = true
 login_redirect = "/index.html"`, 1)
	}
	return strings.Replace(config, `resource "catalyst_appsail" "test" {`, `resource "catalyst_appsail" "test" {`+extra, 1)
}
func TestAccAppSailManagedLanguages(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" {
		t.Skip("requires explicit temporary AppSail deployment opt-in")
	}
	if os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development")
	}
	for _, stack := range []string{"node16", "node18", "node20", "node22", "node24", "python_3_13", "python_3_12", "python_3_11", "python_3_10", "python_3_9", "java25", "java21", "java17", "java11", "java8"} {
		t.Run(stack, func(t *testing.T) { testAccAppSailLanguage(t, stack) })
	}
}
func testAccAppSailLanguage(t *testing.T, stack string) {
	t.Helper()
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(project) {
		t.Fatal("requires isolated project ID")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	registerAppSailBaselineCheck(t, client, project, baseline)
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := t.TempDir()
	first := writeAppSailLanguageArchive(t, dir, stack, "first")
	second := writeAppSailLanguageArchive(t, dir, stack, "second")
	initial := appSailLanguageConfig(project, name, first, stack, false)
	updated := appSailLanguageConfig(project, name, second, stack, true)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, r := range state.RootModule().Resources {
			if r.Type == "catalyst_appsail" {
				_, err := client.GetAppSail(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrAppSailNotFound) {
					return errors.New("temporary AppSail absence unverified")
				}
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail.test", "stack", stack)},
		{ResourceName: "catalyst_appsail.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}, ImportStateIdFunc: func(s *terraform.State) (string, error) {
			return project + "/" + s.RootModule().Resources["catalyst_appsail.test"].Primary.ID, nil
		}},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail.test", "memory", "256"), resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.TEST_VALUE", "fixture"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.%", "0")},
		{Config: initial, PlanOnly: true},
	}})
}
