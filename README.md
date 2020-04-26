# mysql-healthceck
A self-contained binary to run health checks on MySQL and MariaDB clusters.  Supports Percona XtraDB, Galera, and other wsrep-based clustering applications.

## Installing
### Linux amd64
1. Download the `mysql-healthcheck` binary to `/usr/local/bin/`
2. Set `chmod +x /usr/local/bin/mysql-healthcheck`

### Other Platforms
Compile a binary using `go build` for your target OS and architecture.

## Running
When executed from the CLI without the `-d` flag, the application will run in standalone mode.  This can be used for non-HTTP-based health checking needs, or to test your config file.

The `-v` flag enables debug logging.

__Example__:
```
root@database01:~# mysql-healthcheck
time="2020-04-26T05:00:30Z" level=info msg="MySQL cluster node is ready."

root@database01:~# mysql-healthcheck -v
time="2020-04-26T05:00:31Z" level=debug msg="Running in standalone mode."
time="2020-04-26T05:00:31Z" level=debug msg="Config loaded from /etc/default/mysql-healthcheck.yaml"
time="2020-04-26T05:00:31Z" level=debug msg="Constructed DSN for MySQL: testuser:<redacted>@tcp(database01.mydomain.net:3306)/?timeout=1s&tls=true"
time="2020-04-26T05:00:31Z" level=debug msg="Processing standalone health check."
time="2020-04-26T05:00:31Z" level=info msg="MySQL cluster node is ready."
```

To run as a systemd service:
  1. place the included `systemd/mysql-healthcheck.service` unit file in `/etc/systemd/system/`
  2. Run `systemctl enable mysql-healthcheck` to start at boot time.
  3. Run `systemctl start mysql-healthcheck` once you've created a configuration.

## Configuration
### Location
Config files must be located in one of the following locations:
* `/etc/sysconfig/`
* `/etc/default/`
* `$HOME/.config/`
* Current working directory of the application

### Syntax
Config files can be stored in any format supported by [Viper](https://github.com/spf13/viper), including JSON, TOML, YAML, and more.

The config file must be named `mysql-healthcheck` followed by the appropriate suffix for the file format (e.g. `.yaml`, `.json`)

__Example__
```
connection:
  host: database01.mydomain.net
  user: testuser
  password: WA68fARS1TZz2NkK
  tls:
    required: true
    skip-verify: false
    ca: /etc/ssl/certs/my_ca.pem
    cert: /etc/ssl/certs/client_cert.pem
    key: /etc/ssl/private/client_key.pem
options:
  available_when_donor: false
  available_when_readonly: false
  ```

### Parameters
* __connection__: Parameters pertaining to the database connection
    * __host__: The hostname or IP address of the database server (default: `localhost`)
    * __port__: The port to connect to MySQL (default: `3306`)
    * __user__: A username to authenticate to the database server (optional)
    * __password__: The password of the configured user (optional)
    * __tls__: Parameters pertaining to connection-level encryption
        * __required__: If `true`, require TLS encryption on the connection (default: `false`)
        * __skip-verify__: If `true`, accept any certificate without question (default: `false`)
        * __ca__: File path to a trusted CA certificate in PEM format (optional)
        * __cert__: File path to a client certificate in PEM format (optional)
        * __key__: File path to a client private key in PEM format (optional)
* __options__: Parameters pertaining to health checks
    * __available_when_donor__: If `true`, nodes that are donors for SST will be reported as available (default: `false`)
    * __available_when_readonly__: If `true`, nodes that are in read-only mode due to donor activities will be reported as available (default: `false`)
* __http__: Parameters pertaining to running mysql-healthcheck as a service with the `-d` flag
    * __addr__: Address to listen on (default: `::` (All v4/v6 addresses))
    * __port__: Port to bind to (default: `5678`)
    * __path__: URI path to serve health checks at - for example, `/status` or `/health` (default: `/`)