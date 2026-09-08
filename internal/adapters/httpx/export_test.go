package httpx

// Exported for tests in this package's external test file. Keeping the
// production API free of test-only surface is the reason this file exists.
var (
	ChainForTest         = chain
	RecoverPanicForTest  = recoverPanic
	RequestIDForTest     = requestID
	SecureHeadersForTest = secureHeaders
	LimitBodyForTest     = limitBody
	AccessLogForTest     = accessLog
	ContextLoggerForTest = contextLogger
)

// HeaderRequestID is the correlation header name, exported for tests.
const HeaderRequestID = headerRequestID
