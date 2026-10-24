package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

func TestAccKVBucketAndEntry(t *testing.T) {
	testAccPreCheck(t)
	name := randName("kv")
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			checkGone(t, "pulse_kv_bucket", func(ctx context.Context, c *client.Client, a map[string]string) error {
				_, err := c.GetKVBucket(ctx, a["name"])
				return err
			}),
		),
		Steps: []resource.TestStep{
			{
				Config: kvConfig(name, "", `value = "dark"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_kv_bucket.b", "history", "1"),
					resource.TestCheckResourceAttr("pulse_kv_bucket.b", "max_bytes", "-1"),
					resource.TestCheckResourceAttr("pulse_kv_entry.e", "value", "dark"),
					resource.TestCheckResourceAttr("pulse_kv_entry.e", "revision", "1"),
				),
			},
			{
				Config: kvConfig(name, `
  description = "Tool settings"
  history     = 5
  ttl_seconds = 86400
  max_bytes   = 1048576
`, `value = "light"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: append(expectUpdate("pulse_kv_bucket.b").PreApply,
					expectUpdate("pulse_kv_entry.e").PreApply...)},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_kv_bucket.b", "history", "5"),
					resource.TestCheckResourceAttr("pulse_kv_bucket.b", "max_bytes", "1048576"),
					resource.TestCheckResourceAttr("pulse_kv_entry.e", "value", "light"),
					resource.TestCheckResourceAttr("pulse_kv_entry.e", "revision", "2"),
				),
			},
			{ResourceName: "pulse_kv_bucket.b", ImportState: true, ImportStateVerify: true, ImportStateId: name,
				ImportStateVerifyIdentifierAttribute: "name"},
			{ResourceName: "pulse_kv_entry.e", ImportState: true, ImportStateVerify: true, ImportStateId: name + "/ui.theme",
				ImportStateVerifyIdentifierAttribute: "id"},
			// A value changed outside Terraform is put back.
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					_, err := c.PutKVEntry(ctx, name, "ui.theme", "pink", "utf8")
					return err
				}),
				Config:           kvConfig(name, `history = 5`, `value = "light"`),
				ConfigPlanChecks: expectUpdate("pulse_kv_entry.e"),
			},
			// Binary values.
			{
				Config: kvConfig(name, `history = 5`, `
  value    = base64encode("\u0000\u0001binary")
  encoding = "base64"
`),
				Check: resource.TestCheckResourceAttr("pulse_kv_entry.e", "encoding", "base64"),
			},
			{
				Config: kvConfig(name, `history = 5`, `
  value    = base64encode("\u0000\u0001binary")
  encoding = "base64"
`),
				ConfigPlanChecks: expectNoop("pulse_kv_entry.e"),
			},
			{
				PreConfig: oob(t, func(ctx context.Context, c *client.Client) error {
					return c.DeleteKVEntry(ctx, name, "ui.theme", false)
				}),
				Config:           kvConfig(name, `history = 5`, `value = "light"`),
				ConfigPlanChecks: expectRecreate("pulse_kv_entry.e"),
			},
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteKVBucket(ctx, name) }),
				Config:           kvConfig(name, `history = 5`, `value = "light"`),
				ConfigPlanChecks: expectRecreate("pulse_kv_bucket.b"),
			},
		},
	})
}

func kvConfig(name, bucket, entry string) string {
	return fmt.Sprintf(`
resource "pulse_kv_bucket" "b" {
  name = %q
%s
}

resource "pulse_kv_entry" "e" {
  bucket = pulse_kv_bucket.b.name
  key    = "ui.theme"
  %s
}
`, name, bucket, entry)
}

func TestAccObjectBucket(t *testing.T) {
	testAccPreCheck(t)
	name := randName("ob")
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: checkGone(t, "pulse_object_bucket", func(ctx context.Context, c *client.Client, a map[string]string) error {
			_, err := c.GetObjectBucket(ctx, a["name"])
			return err
		}),
		Steps: []resource.TestStep{
			{
				Config: objectBucketConfig(name, ""),
				Check:  resource.TestCheckResourceAttr("pulse_object_bucket.b", "max_bytes", "-1"),
			},
			{
				Config:           objectBucketConfig(name, "description = \"Backups of tool state\"\n  ttl_seconds = 604800\n  max_bytes = 104857600"),
				ConfigPlanChecks: expectUpdate("pulse_object_bucket.b"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pulse_object_bucket.b", "ttl_seconds", "604800"),
					resource.TestCheckResourceAttr("pulse_object_bucket.b", "description", "Backups of tool state"),
				),
			},
			{ResourceName: "pulse_object_bucket.b", ImportState: true, ImportStateVerify: true, ImportStateId: name,
				ImportStateVerifyIdentifierAttribute: "name"},
			{
				PreConfig:        oob(t, func(ctx context.Context, c *client.Client) error { return c.DeleteObjectBucket(ctx, name) }),
				Config:           objectBucketConfig(name, ""),
				ConfigPlanChecks: expectRecreate("pulse_object_bucket.b"),
			},
			{Config: `
resource "pulse_object_bucket" "blobs" {
  name = "blobs"
}
`, ExpectError: regexpMust(`(?i)exists|managed`)},
		},
	})
}

func objectBucketConfig(name, extra string) string {
	return fmt.Sprintf(`
resource "pulse_object_bucket" "b" {
  name = %q
  %s
}
`, name, extra)
}
