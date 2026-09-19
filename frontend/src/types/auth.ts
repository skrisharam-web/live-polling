export interface User {
  id: string
  name: string
  email: string
  createdAt: string
}

export interface Credentials {
  email: string
  password: string
}

export interface Registration extends Credentials {
  name: string
}
