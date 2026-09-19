/// <reference types="vite/client" />

// The two public settings the frontend needs. Declaring them means a typo in
// import.meta.env is a compile error rather than an undefined at runtime.
interface ImportMetaEnv {
  readonly VITE_API_URL: string
  readonly VITE_WS_URL: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
