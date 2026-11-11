"""Load the exact JSON template exposed by the add-on configuration UI."""
import json
from pathlib import Path

import yaml


def load_default_config():
    manifest = Path(__file__).resolve().parents[1] / "caddy" / "config.yaml"
    return json.loads(yaml.safe_load(manifest.read_text())["options"]["caddy_config"])
