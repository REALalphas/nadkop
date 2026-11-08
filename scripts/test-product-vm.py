#!/usr/bin/env python3
"""Compatibility entry point for the guarded current-version VM test runner."""
from pathlib import Path
import runpy
runpy.run_path(str(Path(__file__).with_name('test-product-resume-vm.py')),run_name='__main__')
