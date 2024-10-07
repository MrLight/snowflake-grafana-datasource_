package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/stretchr/testify/require"
)

func TestCheckHealthWithValidConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	req := &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				JSONData:                []byte("{\"account\":\"test\",\"username\":\"user\"}"),
				DecryptedSecureJSONData: map[string]string{"password": "pass"},
			},
		},
	}
	ctx := context.Background()

	service := GetMockService(db)
	service.im.Get(ctx, backend.PluginContext{})
	result, err := service.CheckHealth(ctx, req)
	require.NoError(t, err)
	require.Equal(t, backend.HealthStatusOk, result.Status)
	require.Equal(t, "Data source is working", result.Message)
}

func TestCheckHealthWithInvalidConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnError(sql.ErrConnDone)
	req := &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				JSONData:                []byte("{\"account\":\"invalid\",\"username\":\"user\"}"),
				DecryptedSecureJSONData: map[string]string{"password": "pass"},
			},
		},
	}
	ctx := context.Background()
	service := GetMockService(db)
	service.im.Get(ctx, backend.PluginContext{})
	result, err := service.CheckHealth(ctx, req)
	require.NoError(t, err)
	require.Equal(t, backend.HealthStatusError, result.Status)
	require.Contains(t, result.Message, "Validation query error")
}

func TestCheckHealthWithMissingPasswordAndPrivateKey(t *testing.T) {
	req := &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				JSONData:                []byte("{\"account\":\"test\",\"username\":\"user\"}"),
				DecryptedSecureJSONData: map[string]string{},
			},
		},
	}
	ctx := context.Background()
	td := &SnowflakeDatasource{}
	result, err := td.CheckHealth(ctx, req)
	require.NoError(t, err)
	require.Equal(t, backend.HealthStatusError, result.Status)
	require.Equal(t, "Password or private key are required.", result.Message)
}

func TestCheckHealthWithInvalidJSONData(t *testing.T) {
	req := &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				JSONData:                []byte("{"),
				DecryptedSecureJSONData: map[string]string{"password": "pass"},
			},
		},
	}
	ctx := context.Background()
	td := &SnowflakeDatasource{}
	result, err := td.CheckHealth(ctx, req)
	require.NoError(t, err)
	require.Equal(t, backend.HealthStatusError, result.Status)
	require.Equal(t, "Error getting config: unexpected end of JSON input", result.Message)
}

func TestCreateAndValidationConnectionString(t *testing.T) {

	tcs := []struct {
		request          *backend.CheckHealthRequest
		result           *backend.CheckHealthResult
		connectionString string
	}{
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						DecryptedSecureJSONData: map[string]string{"password": ""},
					},
				},
			},
			result: &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "Password or private key are required."},
		},
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						JSONData:                []byte("{"),
						DecryptedSecureJSONData: map[string]string{"password": "pass"},
					},
				},
			},
			result: &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "Error getting config: unexpected end of JSON input"},
		},
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						JSONData:                []byte("{}"),
						DecryptedSecureJSONData: map[string]string{"password": "pass"},
					},
				},
			},
			result: &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "Account not provided"},
		},
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						JSONData:                []byte("{\"account\":\"test\"}"),
						DecryptedSecureJSONData: map[string]string{"password": "pass"},
					},
				},
			},
			result: &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: "Username not provided"},
		},
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						JSONData:                []byte("{\"account\":\"test\",\"username\":\"user\"}"),
						DecryptedSecureJSONData: map[string]string{"password": "pass"},
					},
				},
			},
			connectionString: "user:pass@test?database=&role=&schema=&warehouse=&validateDefaultParameters=true",
		},
		{
			request: &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
						JSONData:                []byte("{\"account\":\"test\",\"username\":\"user\",\"extraConfig\":\"config=conf\"}"),
						DecryptedSecureJSONData: map[string]string{"password": "pass"},
					},
				},
			},
			connectionString: "user:pass@test?database=&role=&schema=&warehouse=&config=conf&validateDefaultParameters=true",
		},
	}
	for i, tc := range tcs {
		t.Run(fmt.Sprintf("testcase %d", i), func(t *testing.T) {
			con, result := createAndValidationConnectionString(tc.request)
			if result == nil {
				require.Equal(t, tc.connectionString, con)
			} else {
				require.Equal(t, tc.result, result)
			}
		})
	}
}

type FakeInstanceManager struct {
	db *sql.DB
}

func (fakeInstanceManager *FakeInstanceManager) Get(ctx context.Context, setting backend.PluginContext) (instancemgmt.Instance, error) {
	config := pluginConfig{} ///getConfig(&setting)
	return &instanceSettings{db: fakeInstanceManager.db, config: &config}, nil
}

func (*FakeInstanceManager) Do(_ context.Context, _ backend.PluginContext, _ instancemgmt.InstanceCallbackFunc) error {
	return nil
}

func GetMockService(db *sql.DB) *SnowflakeDatasource {
	return &SnowflakeDatasource{
		im: &FakeInstanceManager{db: db},
	}
}
