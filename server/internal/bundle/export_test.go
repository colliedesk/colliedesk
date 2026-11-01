package bundle

// SetBeforeWrite runs fn with the keys of the stored files, in the import's transaction, right after it
// locked and verified them and before it writes a row (nil removes it).
func SetBeforeWrite(fn func(keys []string)) { beforeWrite = fn }

// SetBeforeVerify runs fn with the keys of the stored files before the import's transaction locks them.
func SetBeforeVerify(fn func(keys []string)) { beforeVerify = fn }
