// Copyright 2018-2022 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

// Code generated by mockery v2.53.2. DO NOT EDIT.

package mocks

import (
	context "context"

	grpc "google.golang.org/grpc"

	mock "github.com/stretchr/testify/mock"

	providerv1beta1 "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

// StorageProviderClient is an autogenerated mock type for the StorageProviderClient type
type StorageProviderClient struct {
	mock.Mock
}

type StorageProviderClient_Expecter struct {
	mock *mock.Mock
}

func (_m *StorageProviderClient) EXPECT() *StorageProviderClient_Expecter {
	return &StorageProviderClient_Expecter{mock: &_m.Mock}
}

// ListStorageSpaces provides a mock function with given fields: ctx, in, opts
func (_m *StorageProviderClient) ListStorageSpaces(ctx context.Context, in *providerv1beta1.ListStorageSpacesRequest, opts ...grpc.CallOption) (*providerv1beta1.ListStorageSpacesResponse, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	if len(ret) == 0 {
		panic("no return value specified for ListStorageSpaces")
	}

	var r0 *providerv1beta1.ListStorageSpacesResponse
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, *providerv1beta1.ListStorageSpacesRequest, ...grpc.CallOption) (*providerv1beta1.ListStorageSpacesResponse, error)); ok {
		return rf(ctx, in, opts...)
	}
	if rf, ok := ret.Get(0).(func(context.Context, *providerv1beta1.ListStorageSpacesRequest, ...grpc.CallOption) *providerv1beta1.ListStorageSpacesResponse); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*providerv1beta1.ListStorageSpacesResponse)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, *providerv1beta1.ListStorageSpacesRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// StorageProviderClient_ListStorageSpaces_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ListStorageSpaces'
type StorageProviderClient_ListStorageSpaces_Call struct {
	*mock.Call
}

// ListStorageSpaces is a helper method to define mock.On call
//   - ctx context.Context
//   - in *providerv1beta1.ListStorageSpacesRequest
//   - opts ...grpc.CallOption
func (_e *StorageProviderClient_Expecter) ListStorageSpaces(ctx interface{}, in interface{}, opts ...interface{}) *StorageProviderClient_ListStorageSpaces_Call {
	return &StorageProviderClient_ListStorageSpaces_Call{Call: _e.mock.On("ListStorageSpaces",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *StorageProviderClient_ListStorageSpaces_Call) Run(run func(ctx context.Context, in *providerv1beta1.ListStorageSpacesRequest, opts ...grpc.CallOption)) *StorageProviderClient_ListStorageSpaces_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*providerv1beta1.ListStorageSpacesRequest), variadicArgs...)
	})
	return _c
}

func (_c *StorageProviderClient_ListStorageSpaces_Call) Return(_a0 *providerv1beta1.ListStorageSpacesResponse, _a1 error) *StorageProviderClient_ListStorageSpaces_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *StorageProviderClient_ListStorageSpaces_Call) RunAndReturn(run func(context.Context, *providerv1beta1.ListStorageSpacesRequest, ...grpc.CallOption) (*providerv1beta1.ListStorageSpacesResponse, error)) *StorageProviderClient_ListStorageSpaces_Call {
	_c.Call.Return(run)
	return _c
}

// NewStorageProviderClient creates a new instance of StorageProviderClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewStorageProviderClient(t interface {
	mock.TestingT
	Cleanup(func())
}) *StorageProviderClient {
	mock := &StorageProviderClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
