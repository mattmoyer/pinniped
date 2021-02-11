// Copyright 2020 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "go.pinniped.dev/generated/1.17/apis/concierge/login/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// TokenCredentialRequestLister helps list TokenCredentialRequests.
type TokenCredentialRequestLister interface {
	// List lists all TokenCredentialRequests in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.TokenCredentialRequest, err error)
	// Get retrieves the TokenCredentialRequest from the index for a given name.
	Get(name string) (*v1alpha1.TokenCredentialRequest, error)
	TokenCredentialRequestListerExpansion
}

// tokenCredentialRequestLister implements the TokenCredentialRequestLister interface.
type tokenCredentialRequestLister struct {
	indexer cache.Indexer
}

// NewTokenCredentialRequestLister returns a new TokenCredentialRequestLister.
func NewTokenCredentialRequestLister(indexer cache.Indexer) TokenCredentialRequestLister {
	return &tokenCredentialRequestLister{indexer: indexer}
}

// List lists all TokenCredentialRequests in the indexer.
func (s *tokenCredentialRequestLister) List(selector labels.Selector) (ret []*v1alpha1.TokenCredentialRequest, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.TokenCredentialRequest))
	})
	return ret, err
}

// Get retrieves the TokenCredentialRequest from the index for a given name.
func (s *tokenCredentialRequestLister) Get(name string) (*v1alpha1.TokenCredentialRequest, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("tokencredentialrequest"), name)
	}
	return obj.(*v1alpha1.TokenCredentialRequest), nil
}
