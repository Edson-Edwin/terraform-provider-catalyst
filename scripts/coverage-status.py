#!/usr/bin/env python3
"""Report draft capability progress; discovery is not full lifecycle support."""
import collections
import json
from pathlib import Path

inventory = json.loads((Path(__file__).resolve().parent.parent / 'coverage/capabilities.json').read_text())
counts = collections.Counter(c['status'] for c in inventory['capabilities'])
print('Inventory:', 'complete' if inventory['inventory_complete'] else 'draft; service/field audit incomplete')
print('Capabilities:', len(inventory['capabilities']))
for status in ['managed', 'discovery', 'unimplemented']:
    print(status + ':', counts[status])
print('Terraform resources:', sum(len(c['resources']) for c in inventory['capabilities']))
print('Terraform data sources:', sum(len(c['data_sources']) for c in inventory['capabilities']))
print('10/10 coverage: not established.')
