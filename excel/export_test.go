package excel

// 仅测试使用的内部函数别名，让外部测试包 excel_test 能覆盖装配期校验与打开路径。
var (
	NormalizeColumnsForTest     = normalizeColumns
	ValidateReadOptionsForTest  = ReadOptions.validate
	ValidateWriteOptionsForTest = WriteOptions.validate
	SniffFormatForTest          = sniffFormat
	OpenWorkbookForTest         = openWorkbook
	OpenWorkbookFileForTest     = openWorkbookFile
	ResolveSheetForTest         = resolveSheet
	ConvertValueForTest         = convertValue
)
