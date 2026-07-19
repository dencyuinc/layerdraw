# Native Search Adapter Foundation

This package is the physical adapter layer for Desktop/native Query, Project
Search, Graph Analysis, durable Search Index metadata, and embedding models.
It is reusable by native hosts and intentionally contains no Wails code.

The boundary follows `docs/search-query-and-analysis.md`:

- `NativeExecutor` accepts only verified, opaque, parameterized Engine plans
  and returns bounded typed raw rows. It has no raw Cypher/Ladybug-procedure
  public API and does not construct domain results.
- `DurableIndexStore` records build/active metadata under a complete
  `SearchIndexIdentity`. Activation is atomic; a process restart observes an
  unfinished build as `building`, never as the requested active revision.
- `ConfiguredEmbeddingProvider` accepts only the Engine-produced,
  Access-filtered document text supplied by Runtime. Remote models require an
  explicit host-policy opt-in. Provider/model/profile identity and vector
  dimensions are fixed before use.
- `runtime.SearchService` binds the snapshot, Access projection, Search and
  Embedding profiles, backend version, index schema, and provider output before
  asking Engine to prepare a plan. Engine remains the owner of filtering,
  ranking/fusion, StableAddress binding, cursor construction, QueryResult,
  SearchResult, and AnalysisResult.

Official Desktop composition must provide all values in
`port.RequiredSearchPrimitives`. Missing embeddings remain a typed optional
capability for lexical-only consumers; semantic/hybrid requests fail rather
than silently degrading to lexical search.

The concrete Ladybug driver and local model distribution are release artifacts
injected through `NativeBackend` and `VectorModel`. This keeps native library
code testable and prevents framework shells from acquiring database-native
query or corpus APIs.
