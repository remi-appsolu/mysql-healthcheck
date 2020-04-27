# mysql-healthceck
A self-contained binary to run health checks on MySQL and MariaDB clusters.  Supports Percona XtraDB, Galera, and other wsrep-based clustering applications.

## Installation
### Linux
1. Download the appropriate binary for your architecture to `/usr/local/bin/`
2. Make the binary executable with `chmod +x /usr/local/bin/mysql-healthcheck`
3. Create a configuration file as instructed below.
4. For systemd installation:
    1. Place the `mysql-healthcheck.service` unit file in your systemd system service directory (usually `/etc/systemd/system/`).
    2. Enable the service with `systemd enable mysql-healthcheck.service`
5. Run the application from the command line, or run `systemd start mysql-healthcheck` to start the service.

### Other Platforms
Compile a binary using `go build` for your target OS and architecture.

## Usage
```
  -V    Print version and exit
  -d    Run as a daemon and listen for HTTP connections on a socket
  -v    Verbose (debug) logging
  ```

The application will default to standalone mode, running one check and sending the result to stdout and setting the exit code accordingly.  This can be used for non-HTTP-based health checking needs, or to test changes to your config file.

__Example__:
```
root@database01:~# mysql-healthcheck
time="2020-04-26T05:00:30Z" level=info msg="MySQL cluster node is ready."

root@database01:~# mysql-healthcheck -v
time="2020-04-26T05:00:31Z" level=debug msg="Config loaded from /etc/default/mysql-healthcheck.yaml"
time="2020-04-26T05:00:31Z" level=debug msg="Constructed DSN for MySQL: testuser:<redacted>@tcp(database01.mydomain.net:3306)/?timeout=1s&tls=true"
time="2020-04-26T05:00:31Z" level=debug msg="Running standalone health check."
time="2020-04-26T05:00:31Z" level=info msg="MySQL cluster node is ready."
```

To run as a systemd service:
  1. place the included `systemd/mysql-healthcheck.service` unit file in `/etc/systemd/system/`
  2. Run `systemctl enable mysql-healthcheck` to start at boot time.
  3. Run `systemctl start mysql-healthcheck` once you've created a configuration.

## Configuration
### Location
Config files must be located in one of the following locations:
* Linux
  * `/etc/sysconfig/`
  * `/etc/default/`
  * `/etc/`
  * `$HOME/.config/`
* Windows
  * `%PROGRAMFILES%`
  * `%LOCALAPPDATA%`
* All Platforms
  * Current working directory of the application

### Syntax
Config files can be stored in any format supported by [Viper](https://github.com/spf13/viper), including JSON, TOML, YAML, and more.

The config file must be named `mysql-healthcheck` followed by the appropriate suffix for the file format (e.g. `.yaml`, `.json`)

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
* __http__: Parameters pertaining to running mysql-healthcheck as a service with the `-d` flag
    * __addr__: Address to listen on (default: `::` (All v4/v6 addresses))
    * __port__: Port to bind to (default: `5678`)
    * __path__: URI path to serve health checks at - for example, `/status` or `/health` (default: `/`)
* __options__: Parameters pertaining to health checks
    * __available_when_donor__: If `true`, nodes that are donors for SST will be reported as available (default: `false`)
    * __available_when_readonly__: If `true`, nodes that are in read-only mode due to donor activities will be reported as available (default: `false`)

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
http:
  addr: 10.24.20.48
  port: 8080
  path: /status
options:
  available_when_donor: false
  available_when_readonly: false
```