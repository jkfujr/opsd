import copy
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import container_names


class ContainerNamesTest(unittest.TestCase):
    def test_couchdb_resume_rollback_and_conflict(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            config = {"data_dir": folder, "executor_dir": folder}
            for file, table in (("node.db", "queue"), ("executor.db", "operations")):
                with closing(sqlite3.connect(directory / file)) as db:
                    db.execute("CREATE TABLE " + table + " (terminal INTEGER)")
            state = {"credentials": {"username": "admin", "password": "private"}, "cookie": "cookie", "config": {"enabled": True}}
            container_names.save(directory / "couchdb.json", state)
            current = {"Id": "a" * 64, "Name": "/opsd-couchdb", "Config": {"Labels": {"opsd.storage": folder}}, "State": {"Running": True}, "Mounts": [{"Source": str(directory / "couchdb/data"), "Destination": "/opt/couchdb/data"}, {"Source": str(directory / "couchdb/config"), "Destination": "/opt/couchdb/etc/local.d"}]}
            calls = []

            def run(args):
                if args[0] == "systemctl":
                    return "inactive"
                if args[1] == "ps":
                    return current["Id"]
                if args[1] == "inspect":
                    return json.dumps([current])
                if args[1] == "rename":
                    calls.append(args)
                    saved = container_names.read(directory / "couchdb.json")
                    self.assertEqual(saved["full_id"], current["Id"])
                    current["Name"] = "/" + args[-1]
                    return ""
                self.fail(str(args))

            with patch.object(container_names, "run", run):
                first = container_names.migrate(config, couchdb=True)
                second = container_names.migrate(config, couchdb=True)
                self.assertEqual(first, second)
                self.assertEqual(len(calls), 1)
                container_names.migrate(config, couchdb=True, rollback=True)
                self.assertEqual(current["Name"], "/opsd-couchdb")
                self.assertEqual(len(calls), 2)
            saved = container_names.read(directory / "couchdb.json")
            for key in state:
                self.assertEqual(saved[key], state[key])
            foreign = copy.deepcopy(current)
            foreign["Id"], foreign["Name"] = "b" * 64, "/" + saved["new"]
            foreign["Config"]["Labels"] = {}
            with self.assertRaisesRegex(RuntimeError, "已被占用"):
                container_names.plans(config, [current, foreign], True, False)

    def test_database_ownership_and_handmade_names(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            profile = {"id": "stable_ID", "kind": "galera", "directory": folder}
            container_names.save(directory / "profile.json", profile)
            container_names.save(directory / "deployment.json", {"ID": "stable_ID", "Fingerprint": "spec"})
            managed = {"Id": "a" * 64, "Name": "/opsd-galera-stable_ID", "Config": {"Labels": {"com.opsd.database-deployment": "stable_ID", "com.opsd.database-spec": "spec"}}, "Mounts": [{"Source": str(directory / "database.cnf"), "Destination": "/etc/mysql/conf.d/opsd.cnf"}]}
            handmade = {"Id": "b" * 64, "Name": "/mariadb-galera", "Config": {"Labels": {}}}
            _, _, plans = container_names.plans({"executor_dir": folder}, [managed, handmade], False, False)
            self.assertEqual(plans, [{"full_id": "a" * 64, "old": "opsd-galera-stable_ID", "new": "opsd-database-mariadb-stable_ID"}])
            managed["Config"]["Labels"]["com.opsd.database-spec"] = "different"
            with self.assertRaisesRegex(RuntimeError, "归属不符"):
                container_names.plans({"executor_dir": folder}, [managed], False, False)


if __name__ == "__main__":
    unittest.main()
