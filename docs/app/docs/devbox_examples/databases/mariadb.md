---
title: MariaDB
---
MariaDB can be automatically configured for your dev environment by Devbox via the built-in MariaDB Plugin. This plugin will activate automatically when you install MariaDB using `devbox add mariadb`, or when you use a versioned Nix package like `devbox add mariadb_1010`

[**Example Repo**](https://github.com/jetpack-io/devbox/tree/main/examples/databases/mariadb)

[![Open In Devbox.sh](https://jetpack.io/img/devbox/open-in-devbox.svg)](https://devbox.sh/open/templates/mariadb)

## Adding MariaDB to your Shell

`devbox add mariadb`, or in your `devbox.json` add

```json
    "packages": [
        "mariadb@latest"
    ]
```

You can manually add the MariaDB Plugin to your `devbox.json` by adding it to your `includes` list:

```json
    "includes": [
        "plugin:mariadb"
    ]
```

## MariaDB Plugin Support

Devbox will automatically create the following configuration when you run `devbox add mariadb`. You can view the full configuration by running `devbox info mariadb`


### Services
* mariadb

You can use `devbox services up|stop mariadb` to start or stop the MariaDB Server.

### Environment Variables

```bash
MYSQL_BASEDIR=.devbox/nix/profile/default
MYSQL_HOME=./.devbox/virtenv/mariadb/run
MYSQL_DATADIR=./.devbox/virtenv/mariadb/data
MYSQL_UNIX_PORT=./.devbox/virtenv/mariadb/run/mysql.sock
MYSQL_PID_FILE=./.devbox/mariadb/run/mysql.pid
```

### Files

The plugin will also create the following helper files in your project's `.devbox/virtenv` folder:

* mariadb/flake.nix
* mariadb/setup_db.sh
* mariadb/process-compose.yaml

These files are used to setup your database and service, and should not be modified

### Notes

* This plugin wraps mysqld and mysql_install_db to work in your local project. For more information, see the `flake.nix` created in your `.devbox/virtenv/mariadb` folder.
* This plugin will create a new database for your project in MYSQL_DATADIR if one doesn't exist on shell init.
* You can use `mysqld` to manually start the server, and `mysqladmin -u root shutdown` to manually stop it
* `.sock` filepath can only be maximum 100 characters long. You can point to a different path by setting the `MYSQL_UNIX_PORT` env variable in your `devbox.json` as follows:

```json
"env": {
    "MYSQL_UNIX_PORT": "/<some-other-path>/mysql.sock"
}
```
