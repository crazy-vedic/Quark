// Code generated manually; mirrors what mockgen would produce for store.RequestReader.
package mocks

import (
	"context"
	"reflect"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"go.uber.org/mock/gomock"
)

// Compile-time check that MockRequestReader satisfies the interface it fakes.
var _ store.RequestReader = (*MockRequestReader)(nil)

// MockRequestReader is a mock of store.RequestReader.
type MockRequestReader struct {
	ctrl     *gomock.Controller
	recorder *MockRequestReaderMockRecorder
}

// MockRequestReaderMockRecorder is the mock recorder.
type MockRequestReaderMockRecorder struct {
	mock *MockRequestReader
}

// NewMockRequestReader creates a new mock instance.
func NewMockRequestReader(ctrl *gomock.Controller) *MockRequestReader {
	mock := &MockRequestReader{ctrl: ctrl}
	mock.recorder = &MockRequestReaderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRequestReader) EXPECT() *MockRequestReaderMockRecorder {
	return m.recorder
}

// GetRequest mocks base method.
func (m *MockRequestReader) GetRequest(ctx context.Context, id string) (*domain.Request, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRequest", ctx, id)
	ret0, _ := ret[0].(*domain.Request)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRequest indicates an expected call.
func (mr *MockRequestReaderMockRecorder) GetRequest(ctx, id any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRequest",
		reflect.TypeOf((*MockRequestReader)(nil).GetRequest), ctx, id)
}

// ListRequests mocks base method.
func (m *MockRequestReader) ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListRequests", ctx, collectionID)
	ret0, _ := ret[0].([]*domain.Request)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListRequests indicates an expected call.
func (mr *MockRequestReaderMockRecorder) ListRequests(ctx, collectionID any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListRequests",
		reflect.TypeOf((*MockRequestReader)(nil).ListRequests), ctx, collectionID)
}
