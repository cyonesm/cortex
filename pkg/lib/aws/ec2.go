/*
Copyright 2022 Cortex Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cortexlabs/cortex/pkg/lib/errors"
	"github.com/cortexlabs/cortex/pkg/lib/parallel"
	"github.com/cortexlabs/cortex/pkg/lib/sets/strset"
	s "github.com/cortexlabs/cortex/pkg/lib/strings"
)

var (
	_digitsRegex         = regexp.MustCompile(`[0-9]+`)
	_gpuInstanceFamilies = strset.New("g", "p")
)

type ParsedInstanceType struct {
	Family       string
	Generation   int
	Capabilities strset.Set
	Size         string
}

// Checks weather the input is an AWS instance type
func IsValidInstanceType(instanceType string) bool {
	return AllInstanceTypes.Has(instanceType)
}

// Checks whether the input is an AWS instance type
func CheckValidInstanceType(instanceType string) error {
	if !IsValidInstanceType(instanceType) {
		return ErrorInvalidInstanceType(instanceType)
	}
	return nil
}

// AWS instance types take the form of: [family][generation][capabilities].[size]
// the first group is the instance family, e.g. "m", "t", "g", "inf", ...
// the second group is a generation number for that series, e.g. 3, 4, ...
// the third group is optional, and is a set of single-character capabilities
//
//	"g" represents ARM (graviton), "a" for AMD, "n" for fast networking, "d" for fast storage, etc.
//
// the fourth and final group (after the dot) is the instance size, e.g. "large"
func ParseInstanceType(instanceType string) (ParsedInstanceType, error) {
	if err := CheckValidInstanceType(instanceType); err != nil {
		return ParsedInstanceType{}, err
	}

	parts := strings.Split(instanceType, ".")
	if len(parts) != 2 {
		return ParsedInstanceType{}, errors.ErrorUnexpected("unexpected invalid instance type: " + instanceType)
	}

	prefix := parts[0]
	size := parts[1]

	digitSets := _digitsRegex.FindAllString(prefix, -1)
	if len(digitSets) == 0 {
		return ParsedInstanceType{}, errors.ErrorUnexpected("unexpected invalid instance type: " + instanceType)
	}

	prefixParts := _digitsRegex.Split(prefix, -1)
	capabilitiesStr := prefixParts[len(prefixParts)-1]
	capabilities := strset.FromSlice(strings.Split(capabilitiesStr, ""))

	generationStr := digitSets[len(digitSets)-1]
	generation, ok := s.ParseInt(generationStr)
	if !ok {
		return ParsedInstanceType{}, errors.ErrorUnexpected("unexpected invalid instance type: " + instanceType)
	}

	generationIndex := strings.LastIndex(prefix, generationStr)
	if generationIndex == -1 {
		return ParsedInstanceType{}, errors.ErrorUnexpected("unexpected invalid instance type: " + instanceType)
	}
	family := prefix[:generationIndex]

	return ParsedInstanceType{
		Family:       family,
		Generation:   generation,
		Capabilities: capabilities,
		Size:         size,
	}, nil
}

func IsARMInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if parsedType.Family == "a" {
		return true, nil
	}

	if parsedType.Capabilities.Has("g") {
		return true, nil
	}

	return false, nil
}

func IsAMDGPUInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if !_gpuInstanceFamilies.Has(parsedType.Family) {
		return false, nil
	}

	if parsedType.Capabilities.Has("a") {
		return true, nil
	}

	return false, nil
}

func IsNvidiaGPUInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if !_gpuInstanceFamilies.Has(parsedType.Family) {
		return false, nil
	}

	if !parsedType.Capabilities.Has("a") {
		return true, nil
	}

	return false, nil
}

func IsGPUInstance(instanceType string) (bool, error) {
	isAMDGPU, err := IsAMDGPUInstance(instanceType)
	if err != nil {
		return false, err
	}

	isNvidiaGPU, err := IsNvidiaGPUInstance(instanceType)
	if err != nil {
		return false, err
	}

	return isAMDGPU || isNvidiaGPU, nil
}

func IsInferentiaInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	return parsedType.Family == "inf", nil
}

func IsTrainiumInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	return parsedType.Family == "trn", nil
}

func IsMacInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if parsedType.Family == "mac" {
		return true, nil
	}

	return false, nil
}

func IsFPGAInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if parsedType.Family == "f" {
		return true, nil
	}

	return false, nil
}

func IsAlevoInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if parsedType.Family == "vt" {
		return true, nil
	}

	return false, nil
}

func IsGaudiInstance(instanceType string) (bool, error) {
	parsedType, err := ParseInstanceType(instanceType)
	if err != nil {
		return false, err
	}

	if parsedType.Family == "dl" {
		return true, nil
	}

	return false, nil
}

func (c *Client) SpotInstancePrice(instanceType string) (float64, error) {
	result, err := c.EC2().DescribeSpotPriceHistory(&ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes:       []*string{aws.String(instanceType)},
		ProductDescriptions: []*string{aws.String("Linux/UNIX")},
		StartTime:           aws.Time(time.Now()),
	})
	if err != nil {
		return 0, errors.Wrap(err, "checking spot instance price")
	}

	min := math.MaxFloat64

	for _, spotPrice := range result.SpotPriceHistory {
		if spotPrice == nil {
			continue
		}

		price, ok := s.ParseFloat64(*spotPrice.SpotPrice)
		if !ok {
			continue
		}

		if price < min {
			min = price
		}
	}

	if min == math.MaxFloat64 {
		return 0, ErrorNoValidSpotPrices(instanceType, c.Region)
	}

	if min <= 0 {
		return 0, ErrorNoValidSpotPrices(instanceType, c.Region)
	}

	return min, nil
}

func (c *Client) ListAllRegions() (strset.Set, error) {
	result, err := c.EC2().DescribeRegions(&ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(true),
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	regions := strset.New()
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions.Add(*region.RegionName)
		}
	}

	return regions, nil
}

// Returns only regions that are enabled for your account
func (c *Client) ListEnabledRegions() (strset.Set, error) {
	result, err := c.EC2().DescribeRegions(&ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	regions := strset.New()
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions.Add(*region.RegionName)
		}
	}

	return regions, nil
}

// Returns all regions and enabled regions
func (c *Client) ListRegions() (strset.Set, strset.Set, error) {
	var allRegions strset.Set
	var enabledRegions strset.Set

	err := parallel.RunFirstErr(
		func() error {
			var err error
			allRegions, err = c.ListAllRegions()
			return err
		},
		func() error {
			var err error
			enabledRegions, err = c.ListEnabledRegions()
			return err
		},
	)

	if err != nil {
		return nil, nil, err
	}

	return allRegions, enabledRegions, nil
}

func (c *Client) ListAvailabilityZonesInRegion() (strset.Set, error) {
	input := &ec2.DescribeAvailabilityZonesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("region-name"),
				Values: []*string{aws.String(c.Region)},
			},
			{
				Name:   aws.String("state"),
				Values: []*string{aws.String(ec2.AvailabilityZoneStateAvailable)},
			},
		},
	}

	result, err := c.EC2().DescribeAvailabilityZones(input)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	zones := strset.New()
	for _, az := range result.AvailabilityZones {
		if az.ZoneName != nil {
			zones.Add(*az.ZoneName)
		}
	}

	return zones, nil
}

func (c *Client) listSupportedAvailabilityZonesSingle(instanceType string) (strset.Set, error) {
	input := &ec2.DescribeReservedInstancesOfferingsInput{
		InstanceType:       &instanceType,
		IncludeMarketplace: aws.Bool(false),
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("scope"),
				Values: []*string{aws.String(ec2.ScopeAvailabilityZone)},
			},
		},
	}

	zones := strset.New()
	err := c.EC2().DescribeReservedInstancesOfferingsPages(input, func(output *ec2.DescribeReservedInstancesOfferingsOutput, lastPage bool) bool {
		for _, offering := range output.ReservedInstancesOfferings {
			if offering.AvailabilityZone != nil {
				zones.Add(*offering.AvailabilityZone)
			}
		}
		return true
	})

	if err != nil {
		return nil, errors.WithStack(err)
	}

	return zones, nil
}

func (c *Client) ListSupportedAvailabilityZones(instanceType string, instanceTypes ...string) (strset.Set, error) {
	allInstanceTypes := append(instanceTypes, instanceType)
	zoneSets := make([]strset.Set, len(allInstanceTypes))
	fns := make([]func() error, len(allInstanceTypes))

	for i := range allInstanceTypes {
		localIdx := i
		fns[i] = func() error {
			zones, err := c.listSupportedAvailabilityZonesSingle(allInstanceTypes[localIdx])
			if err != nil {
				return err
			}
			zoneSets[localIdx] = zones
			return nil
		}
	}

	err := parallel.RunFirstErr(fns[0], fns[1:]...)
	if err != nil {
		return nil, err
	}

	return strset.Intersection(zoneSets...), nil
}

func (c *Client) ListElasticIPs() ([]string, error) {
	addresses, err := c.EC2().DescribeAddresses(&ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	addressesList := []string{}
	if addresses != nil {
		for _, address := range addresses.Addresses {
			if address != nil && address.PublicIp != nil {
				addressesList = append(addressesList, *address.PublicIp)
			}
		}
	}

	return addressesList, nil
}

func (c *Client) ListInternetGateways() ([]string, error) {
	gatewaysList := []string{}
	err := c.EC2().DescribeInternetGatewaysPages(&ec2.DescribeInternetGatewaysInput{}, func(output *ec2.DescribeInternetGatewaysOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, gateway := range output.InternetGateways {
			if gateway != nil && gateway.InternetGatewayId != nil {
				gatewaysList = append(gatewaysList, *gateway.InternetGatewayId)
			}
		}

		return true
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return gatewaysList, nil
}

func (c *Client) DescribeNATGateways() ([]ec2.NatGateway, error) {
	var gateways []ec2.NatGateway
	err := c.EC2().DescribeNatGatewaysPages(&ec2.DescribeNatGatewaysInput{}, func(output *ec2.DescribeNatGatewaysOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, gateway := range output.NatGateways {
			if gateway == nil {
				continue
			}
			gateways = append(gateways, *gateway)
		}

		return true
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return gateways, nil
}

func (c *Client) DescribeSubnets() ([]ec2.Subnet, error) {
	var subnets []ec2.Subnet
	err := c.EC2().DescribeSubnetsPages(&ec2.DescribeSubnetsInput{}, func(output *ec2.DescribeSubnetsOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, subnet := range output.Subnets {
			if subnet == nil {
				continue
			}
			subnets = append(subnets, *subnet)
		}

		return true
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return subnets, nil
}

func (c *Client) DescribeVpcs() ([]ec2.Vpc, error) {
	var vpcs []ec2.Vpc
	err := c.EC2().DescribeVpcsPages(&ec2.DescribeVpcsInput{}, func(output *ec2.DescribeVpcsOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, vpc := range output.Vpcs {
			if vpc == nil {
				continue
			}
			vpcs = append(vpcs, *vpc)
		}

		return true
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return vpcs, nil
}

func (c *Client) DescribeSecurityGroups() ([]ec2.SecurityGroup, error) {
	var sgs []ec2.SecurityGroup
	err := c.EC2().DescribeSecurityGroupsPages(&ec2.DescribeSecurityGroupsInput{}, func(output *ec2.DescribeSecurityGroupsOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, sg := range output.SecurityGroups {
			if sg == nil {
				continue
			}
			sgs = append(sgs, *sg)
		}

		return true
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return sgs, nil
}

func (c *Client) ListVolumes(tags ...ec2.Tag) ([]ec2.Volume, error) {
	var volumes []ec2.Volume
	err := c.EC2().DescribeVolumesPages(&ec2.DescribeVolumesInput{}, func(output *ec2.DescribeVolumesOutput, lastPage bool) bool {
		if output == nil {
			return false
		}
		for _, volume := range output.Volumes {
			if volume == nil {
				continue
			}
			if hasAllEC2Tags(tags, volume.Tags) {
				volumes = append(volumes, *volume)
			}
		}

		return true
	})

	if err != nil {
		return nil, errors.WithStack(err)
	}

	return volumes, nil
}

func (c *Client) DeleteVolume(volumeID string) error {
	_, err := c.EC2().DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: aws.String(volumeID),
	})
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func hasAllEC2Tags(queryTags []ec2.Tag, allResourceTags []*ec2.Tag) bool {
	for _, queryTag := range queryTags {
		if !hasEC2Tag(queryTag, allResourceTags) {
			return false
		}
	}
	return true
}

// if queryTag's value is nil, the tag will match as long as the key is present in the resource's tags
func hasEC2Tag(queryTag ec2.Tag, allResourceTags []*ec2.Tag) bool {
	for _, resourceTag := range allResourceTags {
		if queryTag.Key != nil && resourceTag.Key != nil && *queryTag.Key == *resourceTag.Key {
			if queryTag.Value == nil {
				return true
			}
			if queryTag.Value != nil && resourceTag.Value != nil && *queryTag.Value == *resourceTag.Value {
				return true
			}
		}
	}
	return false
}
