// Copyright (c) 2017 Huawei Technologies Co., Ltd. All Rights Reserved.
//
//    Licensed under the Apache License, Version 2.0 (the "License"); you may
//    not use this file except in compliance with the License. You may obtain
//    a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
//    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
//    License for the specific language governing permissions and limitations
//    under the License.

/*
This module implements cinder driver for OpenSDS. Cinder driver will pass
these operation requests about volume to gophercloud which is an OpenStack
Go SDK.

*/

package cinder

import (
	"io/ioutil"

	log "github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/schedulerstats"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"
	snapshotsv2 "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/snapshots"
	volumesv2 "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	pb "github.com/opensds/opensds/pkg/dock/proto"
	"github.com/opensds/opensds/pkg/model"
	"github.com/opensds/opensds/pkg/utils/config"
	"github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"
)

var conf = CinderConfig{}

type Driver struct {
	// Current block storage version
	blockStoragev2 *gophercloud.ServiceClient
	blockStoragev3 *gophercloud.ServiceClient

	config CinderConfig
}

type AuthOptions struct {
	IdentityEndpoint string `yaml:"endpoint,omitempty"`
	DomainID         string `yaml:"domainId,omitempty"`
	DomainName       string `yaml:"domainName,omitempty"`
	Username         string `yaml:"username,omitempty"`
	Password         string `yaml:"password,omitempty"`
	TenantID         string `yaml:"tenantId,omitempty"`
	TenantName       string `yaml:"tenantName,omitempty"`
}

type CinderConfig struct {
	AuthOptions `yaml:"authOptions"`
}

func (d *Driver) Setup() error {
	// Read cinder config file
	confYaml, err := ioutil.ReadFile(config.CONF.CinderConfig)
	if err != nil {
		log.Fatalf("Read ceph config yaml file (%s) failed, reason:(%v)", config.CONF.CinderConfig, err)
		return err
	}
	err = yaml.Unmarshal([]byte(confYaml), &conf)
	if err != nil {
		log.Fatal("Parse error: %v", err)
		return err
	}
	d.config = conf

	opts := gophercloud.AuthOptions{
		IdentityEndpoint: d.config.IdentityEndpoint,
		DomainID:         d.config.DomainID,
		DomainName:       d.config.DomainName,
		Username:         d.config.Username,
		Password:         d.config.Password,
		TenantID:         d.config.TenantID,
		TenantName:       d.config.TenantName,
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		log.Error("When get auth options:", err)
		return err
	}

	d.blockStoragev2, err = openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		log.Error("When get block storage session:", err)
		return err
	}
	return nil
}

func (d *Driver) Unset() error { return nil }

func (d *Driver) CreateVolume(req *pb.CreateVolumeOpts) (*model.VolumeSpec, error) {
	//Configure create request body.
	opts := &volumesv2.CreateOpts{
		Name:             req.GetName(),
		Description:      req.GetDescription(),
		Size:             int(req.GetSize()),
		AvailabilityZone: req.GetAvailabilityZone(),
	}

	vol, err := volumesv2.Create(d.blockStoragev2, opts).Extract()
	if err != nil {
		log.Error("Cannot create volume:", err)
		return nil, err
	}

	return &model.VolumeSpec{
		BaseModel: &model.BaseModel{
			Id: vol.ID,
		},
		Name:             vol.Name,
		Description:      vol.Description,
		Size:             int64(vol.Size),
		AvailabilityZone: vol.AvailabilityZone,
		Status:           vol.Status,
	}, nil
}

func (d *Driver) PullVolume(volID string) (*model.VolumeSpec, error) {
	vol, err := volumesv2.Get(d.blockStoragev2, volID).Extract()
	if err != nil {
		log.Error("Cannot get volume:", err)
		return nil, err
	}

	return &model.VolumeSpec{
		BaseModel: &model.BaseModel{
			Id: vol.ID,
		},
		Name:             vol.Name,
		Description:      vol.Description,
		Size:             int64(vol.Size),
		AvailabilityZone: vol.AvailabilityZone,
		Status:           vol.Status,
	}, nil
}

func (d *Driver) DeleteVolume(opt *pb.DeleteVolumeOpts) error {
	if err := volumesv2.Delete(d.blockStoragev2, opt.GetId()).ExtractErr(); err != nil {
		log.Error("Cannot delete volume:", err)
		return err
	}

	return nil
}

func (d *Driver) InitializeConnection(req *pb.CreateAttachmentOpts) (*model.ConnectionInfo, error) {
	opts := &volumeactions.InitializeConnectionOpts{
		IP:        req.HostInfo.GetIp(),
		Host:      req.HostInfo.GetHost(),
		Initiator: req.HostInfo.GetInitiator(),
		Platform:  req.HostInfo.GetPlatform(),
		OSType:    req.HostInfo.GetOsType(),
		Multipath: &req.MultiPath,
	}

	conn, err := volumeactions.InitializeConnection(d.blockStoragev2, req.GetVolumeId(), opts).Extract()
	if err != nil {
		log.Error("Cannot initialize volume connection:", err)
		return nil, err
	}

	return &model.ConnectionInfo{
		DriverVolumeType: "iscsi",
		ConnectionData:   conn,
	}, nil
}

func (d *Driver) CreateSnapshot(req *pb.CreateVolumeSnapshotOpts) (*model.VolumeSnapshotSpec, error) {
	opts := &snapshotsv2.CreateOpts{
		VolumeID:    req.GetVolumeId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
	}

	snp, err := snapshotsv2.Create(d.blockStoragev2, opts).Extract()
	if err != nil {
		log.Error("Cannot create snapshot:", err)
		return nil, err
	}

	return &model.VolumeSnapshotSpec{
		BaseModel: &model.BaseModel{
			Id: snp.ID,
		},
		Name:        snp.Name,
		Description: snp.Description,
		Size:        int64(snp.Size),
		Status:      snp.Status,
		VolumeId:    req.GetVolumeId(),
	}, nil
}

func (d *Driver) PullSnapshot(snapID string) (*model.VolumeSnapshotSpec, error) {
	snp, err := snapshotsv2.Get(d.blockStoragev2, snapID).Extract()
	if err != nil {
		log.Error("Cannot get snapshot:", err)
		return nil, err
	}

	return &model.VolumeSnapshotSpec{
		BaseModel: &model.BaseModel{
			Id: snp.ID,
		},
		Name:        snp.Name,
		Description: snp.Description,
		Size:        int64(snp.Size),
		Status:      snp.Status,
		VolumeId:    snp.VolumeID,
	}, nil
}

func (d *Driver) DeleteSnapshot(req *pb.DeleteVolumeSnapshotOpts) error {
	if err := snapshotsv2.Delete(d.blockStoragev2, req.GetId()).ExtractErr(); err != nil {
		log.Error("Cannot delete snapshot:", err)
		return err
	}

	return nil
}

func (d *Driver) ListPools() ([]*model.StoragePoolSpec, error) {
	opts := &schedulerstats.ListOpts{}

	pages, err := schedulerstats.List(d.blockStoragev2, opts).AllPages()
	if err != nil {
		log.Error("Cannot list storage pools:", err)
		return nil, err
	}

	polpages, err := schedulerstats.ExtractStoragePools(pages)
	if err != nil {
		log.Error("annot extract storage pools:", err)
		return nil, err
	}

	var pols []*model.StoragePoolSpec
	for _, page := range polpages {
		pol := &model.StoragePoolSpec{
			BaseModel: &model.BaseModel{
				Id: uuid.NewV5(uuid.NamespaceOID, page.Name).String(),
			},
			Name:          page.Name,
			TotalCapacity: int64(page.Capabilities.TotalCapacityGB),
			FreeCapacity:  int64(page.Capabilities.FreeCapacityGB),
		}

		pols = append(pols, pol)
	}
	return pols, nil
}

