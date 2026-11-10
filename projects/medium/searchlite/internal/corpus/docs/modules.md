# Modules

A module is a tree of packages versioned together, described by a go.mod file at its root. The file records the module path, the minimum Go version and the dependencies the build needs. Running go mod tidy adds what the code imports and removes what it no longer uses. Go resolves versions with minimal version selection: it picks the oldest release that satisfies every requirement, so builds are reproducible without a lock file. A checksum database guards downloaded modules against tampering. Major versions above one become part of the import path.
