"""Tests of evidence classification, independent of provider behaviour."""
import json
import tempfile
import unittest
from pathlib import Path
from schema_matrix_report import classify, summarise


class MatrixEvidenceTest(unittest.TestCase):
    def row(self, **overrides):
        row = {"Route":"route", "Model":"synthetic", "Case":"enum", "Mode":"default", "Prompt":"valid",
               "Statuses":[200], "Arguments":[{"value":"red"}], "OriginalValid":[True],
               "RequestedMatch":[True], "ToolNames":["schema_probe"]}
        row.update(overrides)
        return row

    def test_valid_arguments_do_not_hide_protocol_failure(self):
        for override, expected in [({"ToolNames":["another_tool"]}, "wrong-tool"),
                                   ({"ResponseErrors":["stream failed"]}, "response-error"),
                                   ({"FinishReasons":["MAX_TOKENS"]}, "truncated"),
                                   ({"Arguments":[]}, "no-call"),
                                   ({"Arguments":[{},{}]}, "multiple-calls"),
                                   ({"CallError":"upstream rejected", "Statuses":[400]}, "http-400")]:
            self.assertEqual(classify(self.row(**override)), expected)
        self.assertEqual(classify(self.row()), "conforming")
        self.assertEqual(classify(self.row(CollectionError="model setup failed")), "collection-error")

    def test_original_contract_is_checked_after_fallback(self):
        self.assertEqual(classify(self.row(Mode="fallback", OriginalValid=[False], PreparedValid=[True])), "nonconforming")
        self.assertEqual(classify(self.row(RequestedMatch=[False])), "valid-but-changed")

    def test_partial_results_cannot_be_reported_as_complete(self):
        with tempfile.TemporaryDirectory() as name:
            directory=Path(name)
            (directory/'row.json').write_text(json.dumps(self.row()))
            manifest={"format_version":2,"planned":["route/enum/default/valid","route/enum/default/conflict"]}
            (directory/'manifest.json').write_text(json.dumps(manifest))
            with self.assertRaises(ValueError):
                summarise(directory)
            self.assertFalse(summarise(directory,True)['complete'])
            (directory/'other.json').write_text(json.dumps(self.row(Prompt="conflict")))
            self.assertTrue(summarise(directory)['complete'])
            (directory/'other.json').write_text(json.dumps(self.row(Prompt="conflict",CollectionError="model setup failed")))
            with self.assertRaises(ValueError):
                summarise(directory)

if __name__ == '__main__':
    unittest.main()
