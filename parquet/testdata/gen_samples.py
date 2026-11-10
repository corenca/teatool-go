#!/usr/bin/env python3
"""生成 parquet 包的互操作测试样本。

依赖 pyarrow（不在 Go 依赖里）：

    python3 -m pip install pyarrow
    python3 testdata/gen_samples.py

生成的文件入库（每个几 KB）：
    spark_like.parquet  常见 Spark/Pandas 类型：int64/string/double/timestamp[us,UTC]/date32/list/struct/可空
    decimal.parquet     decimal128(10,2)
    nested.parquet      嵌套 struct / list<struct> / map<string,int32>

INT96 时间戳样本不生成：上游只能读成 deprecated.Int96，本包不支持还原成 time.Time。
"""

import datetime as dt
from decimal import Decimal

import pyarrow as pa
import pyarrow.parquet as pq


def write_spark_like() -> None:
    """写一份接近 Spark 产出的文件。"""
    schema = pa.schema(
        [
            ("id", pa.int64()),
            ("name", pa.string()),
            ("score", pa.float64()),
            ("ts", pa.timestamp("us", tz="UTC")),
            ("day", pa.date32()),
            ("tags", pa.list_(pa.string())),
            ("info", pa.struct([("city", pa.string())])),
            ("note", pa.string()),
        ]
    )
    table = pa.table(
        {
            "id": [1, 2],
            "name": ["张三", "李四"],
            "score": [98.5, 90.0],
            "ts": [
                dt.datetime(2026, 11, 10, 9, 30, tzinfo=dt.timezone.utc),
                dt.datetime(2026, 11, 10, 10, 0, tzinfo=dt.timezone.utc),
            ],
            "day": [dt.date(2026, 11, 10), dt.date(2026, 11, 11)],
            "tags": [["a", "b"], []],
            "info": [{"city": "北京"}, {"city": "上海"}],
            "note": [None, "备注"],
        },
        schema=schema,
    )
    pq.write_table(table, "spark_like.parquet", compression="snappy")
    print("写出 spark_like.parquet", table.num_rows, "行")


def write_decimal() -> None:
    """写 decimal(10,2) 列，验证动态读取的物理值形态。"""
    schema = pa.schema([("amount", pa.decimal128(10, 2))])
    table = pa.table({"amount": [Decimal("1234.50")]}, schema=schema)
    pq.write_table(table, "decimal.parquet", compression="snappy")
    print("写出 decimal.parquet", table.num_rows, "行")


def write_nested() -> None:
    """写嵌套 struct / list<struct> / map<string,int32>。"""
    schema = pa.schema(
        [
            (
                "outer",
                pa.struct([("id", pa.int64()), ("inner", pa.struct([("city", pa.string())]))]),
            ),
            ("items", pa.list_(pa.struct([("x", pa.int32()), ("y", pa.string())]))),
            ("attrs", pa.map_(pa.string(), pa.int32())),
        ]
    )
    table = pa.table(
        {
            "outer": [{"id": 1, "inner": {"city": "北京"}}],
            "items": [[{"x": 7, "y": "a"}]],
            "attrs": [[("k", 5)]],
        },
        schema=schema,
    )
    pq.write_table(table, "nested.parquet", compression="zstd")
    print("写出 nested.parquet", table.num_rows, "行")


def main() -> None:
    write_spark_like()
    write_decimal()
    write_nested()


if __name__ == "__main__":
    main()
