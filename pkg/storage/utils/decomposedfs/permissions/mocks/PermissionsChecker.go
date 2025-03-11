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

	node "github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/node"
	mock "github.com/stretchr/testify/mock"

	providerv1beta1 "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

// PermissionsChecker is an autogenerated mock type for the PermissionsChecker type
type PermissionsChecker struct {
	mock.Mock
}

type PermissionsChecker_Expecter struct {
	mock *mock.Mock
}

func (_m *PermissionsChecker) EXPECT() *PermissionsChecker_Expecter {
	return &PermissionsChecker_Expecter{mock: &_m.Mock}
}

// AssemblePermissions provides a mock function with given fields: ctx, n
func (_m *PermissionsChecker) AssemblePermissions(ctx context.Context, n *node.Node) (*providerv1beta1.ResourcePermissions, error) {
	ret := _m.Called(ctx, n)

	if len(ret) == 0 {
		panic("no return value specified for AssemblePermissions")
	}

	var r0 *providerv1beta1.ResourcePermissions
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, *node.Node) (*providerv1beta1.ResourcePermissions, error)); ok {
		return rf(ctx, n)
	}
	if rf, ok := ret.Get(0).(func(context.Context, *node.Node) *providerv1beta1.ResourcePermissions); ok {
		r0 = rf(ctx, n)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*providerv1beta1.ResourcePermissions)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, *node.Node) error); ok {
		r1 = rf(ctx, n)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// PermissionsChecker_AssemblePermissions_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'AssemblePermissions'
type PermissionsChecker_AssemblePermissions_Call struct {
	*mock.Call
}

// AssemblePermissions is a helper method to define mock.On call
//   - ctx context.Context
//   - n *node.Node
func (_e *PermissionsChecker_Expecter) AssemblePermissions(ctx interface{}, n interface{}) *PermissionsChecker_AssemblePermissions_Call {
	return &PermissionsChecker_AssemblePermissions_Call{Call: _e.mock.On("AssemblePermissions", ctx, n)}
}

func (_c *PermissionsChecker_AssemblePermissions_Call) Run(run func(ctx context.Context, n *node.Node)) *PermissionsChecker_AssemblePermissions_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*node.Node))
	})
	return _c
}

func (_c *PermissionsChecker_AssemblePermissions_Call) Return(ap *providerv1beta1.ResourcePermissions, err error) *PermissionsChecker_AssemblePermissions_Call {
	_c.Call.Return(ap, err)
	return _c
}

func (_c *PermissionsChecker_AssemblePermissions_Call) RunAndReturn(run func(context.Context, *node.Node) (*providerv1beta1.ResourcePermissions, error)) *PermissionsChecker_AssemblePermissions_Call {
	_c.Call.Return(run)
	return _c
}

// AssembleTrashPermissions provides a mock function with given fields: ctx, n
func (_m *PermissionsChecker) AssembleTrashPermissions(ctx context.Context, n *node.Node) (*providerv1beta1.ResourcePermissions, error) {
	ret := _m.Called(ctx, n)

	if len(ret) == 0 {
		panic("no return value specified for AssembleTrashPermissions")
	}

	var r0 *providerv1beta1.ResourcePermissions
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, *node.Node) (*providerv1beta1.ResourcePermissions, error)); ok {
		return rf(ctx, n)
	}
	if rf, ok := ret.Get(0).(func(context.Context, *node.Node) *providerv1beta1.ResourcePermissions); ok {
		r0 = rf(ctx, n)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*providerv1beta1.ResourcePermissions)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, *node.Node) error); ok {
		r1 = rf(ctx, n)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// PermissionsChecker_AssembleTrashPermissions_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'AssembleTrashPermissions'
type PermissionsChecker_AssembleTrashPermissions_Call struct {
	*mock.Call
}

// AssembleTrashPermissions is a helper method to define mock.On call
//   - ctx context.Context
//   - n *node.Node
func (_e *PermissionsChecker_Expecter) AssembleTrashPermissions(ctx interface{}, n interface{}) *PermissionsChecker_AssembleTrashPermissions_Call {
	return &PermissionsChecker_AssembleTrashPermissions_Call{Call: _e.mock.On("AssembleTrashPermissions", ctx, n)}
}

func (_c *PermissionsChecker_AssembleTrashPermissions_Call) Run(run func(ctx context.Context, n *node.Node)) *PermissionsChecker_AssembleTrashPermissions_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*node.Node))
	})
	return _c
}

func (_c *PermissionsChecker_AssembleTrashPermissions_Call) Return(ap *providerv1beta1.ResourcePermissions, err error) *PermissionsChecker_AssembleTrashPermissions_Call {
	_c.Call.Return(ap, err)
	return _c
}

func (_c *PermissionsChecker_AssembleTrashPermissions_Call) RunAndReturn(run func(context.Context, *node.Node) (*providerv1beta1.ResourcePermissions, error)) *PermissionsChecker_AssembleTrashPermissions_Call {
	_c.Call.Return(run)
	return _c
}

// NewPermissionsChecker creates a new instance of PermissionsChecker. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewPermissionsChecker(t interface {
	mock.TestingT
	Cleanup(func())
}) *PermissionsChecker {
	mock := &PermissionsChecker{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
