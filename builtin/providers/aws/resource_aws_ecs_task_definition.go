package aws

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceAwsEcsTaskDefinition() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsEcsTaskDefinitionCreate,
		Read:   resourceAwsEcsTaskDefinitionRead,
		Update: resourceAwsEcsTaskDefinitionUpdate,
		Delete: resourceAwsEcsTaskDefinitionDelete,

		Schema: map[string]*schema.Schema{
			"arn": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"family": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"revision": &schema.Schema{
				Type:     schema.TypeInt,
				Computed: true,
			},

			"container_definitions": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				StateFunc: func(v interface{}) string {
					hash := sha1.Sum([]byte(v.(string)))
					return hex.EncodeToString(hash[:])
				},
			},

			"volume": &schema.Schema{
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},

						"host_path": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
				Set: resourceAwsEcsTaskDefinitionVolumeHash,
			},
		},
	}
}

func resourceAwsEcsTaskDefinitionCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	rawDefinitions := d.Get("container_definitions").(string)
	definitions, err := expandEcsContainerDefinitions(rawDefinitions)
	if err != nil {
		return err
	}

	input := ecs.RegisterTaskDefinitionInput{
		ContainerDefinitions: definitions,
		Family:               aws.String(d.Get("family").(string)),
	}

	if v, ok := d.GetOk("volume"); ok {
		volumes, err := expandEcsVolumes(v.(*schema.Set).List())
		if err != nil {
			return err
		}
		input.Volumes = volumes
	}

	log.Printf("[DEBUG] Registering ECS task definition: %s", awsutil.StringValue(input))
	out, err := conn.RegisterTaskDefinition(&input)
	if err != nil {
		return err
	}

	taskDefinition := *out.TaskDefinition

	log.Printf("[DEBUG] ECS task definition registered: %q (rev. %d)",
		*taskDefinition.TaskDefinitionARN, *taskDefinition.Revision)

	d.SetId(*taskDefinition.Family)
	d.Set("arn", *taskDefinition.TaskDefinitionARN)

	return resourceAwsEcsTaskDefinitionRead(d, meta)
}

func resourceAwsEcsTaskDefinitionRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	log.Printf("[DEBUG] Reading task definition %s", d.Id())
	out, err := conn.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(d.Get("arn").(string)),
	})
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] Received task definition %s", awsutil.StringValue(out))

	taskDefinition := out.TaskDefinition

	d.SetId(*taskDefinition.Family)
	d.Set("arn", *taskDefinition.TaskDefinitionARN)
	d.Set("family", *taskDefinition.Family)
	d.Set("revision", *taskDefinition.Revision)
	d.Set("container_definitions", taskDefinition.ContainerDefinitions)
	d.Set("volumes", flattenEcsVolumes(taskDefinition.Volumes))

	return nil
}

func resourceAwsEcsTaskDefinitionUpdate(d *schema.ResourceData, meta interface{}) error {
	oldArn := d.Get("arn").(string)

	log.Printf("[DEBUG] Creating new revision of task definition %q", d.Id())
	err := resourceAwsEcsTaskDefinitionCreate(d, meta)
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] New revision of %q created: %q", d.Id(), d.Get("arn").(string))

	log.Printf("[DEBUG] Deregistering old revision of task definition %q: %q", d.Id(), oldArn)
	conn := meta.(*AWSClient).ecsconn
	_, err = conn.DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(oldArn),
	})
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] Old revision of task definition deregistered: %q", oldArn)

	return resourceAwsEcsTaskDefinitionRead(d, meta)
}

func resourceAwsEcsTaskDefinitionDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	_, err := conn.DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(d.Get("arn").(string)),
	})
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] Task definition %q deregistered.", d.Get("arn").(string))

	return nil
}

func resourceAwsEcsTaskDefinitionVolumeHash(v interface{}) int {
	var buf bytes.Buffer
	m := v.(map[string]interface{})
	buf.WriteString(fmt.Sprintf("%s-", m["name"].(string)))
	buf.WriteString(fmt.Sprintf("%s-", m["host_path"].(string)))

	return hashcode.String(buf.String())
}
