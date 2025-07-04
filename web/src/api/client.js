class ApiClient {
  constructor(baseURL = '/api') {
    this.baseURL = baseURL
    this.eventSource = null
    this.listeners = new Map()
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = 5
    this.reconnectInterval = 1000
  }

  async request(endpoint, options = {}) {
    const url = `${this.baseURL}${endpoint}`
    const config = {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers
      },
      ...options
    }

    try {
      const response = await fetch(url, config)
      
      if (!response.ok) {
        const errorData = await response.json().catch(() => ({ error: 'Unknown error' }))
        throw new Error(errorData.error || `HTTP ${response.status}`)
      }

      // Handle no content responses
      if (response.status === 204) {
        return null
      }

      return await response.json()
    } catch (error) {
      console.error(`API request failed: ${endpoint}`, error)
      throw error
    }
  }

  // Repository endpoints
  async getRepositories() {
    return this.request('/repositories')
  }

  async addRepository(path, name) {
    return this.request('/repositories', {
      method: 'POST',
      body: JSON.stringify({ path, name })
    })
  }

  async removeRepository(id) {
    return this.request(`/repositories/${id}`, {
      method: 'DELETE'
    })
  }

  // Status endpoints
  async getStatus() {
    return this.request('/status')
  }

  // IDE integration
  async openInIDE(path) {
    return this.request('/actions/open-ide', {
      method: 'POST',
      body: JSON.stringify({ path })
    })
  }

  // Directory suggestions
  async getDirectorySuggestions(query) {
    return this.request(`/suggestions/directories?q=${encodeURIComponent(query)}`)
  }

  // Hook management
  async getHookStatus(path) {
    return this.request(`/hooks/status?path=${encodeURIComponent(path)}`)
  }

  async installHook(path) {
    return this.request('/hooks/install', {
      method: 'POST',
      body: JSON.stringify({ path })
    })
  }

  // Webhook endpoint (for Claude integration)
  async sendWebhook(data) {
    return this.request('/webhook/claude', {
      method: 'POST',
      body: JSON.stringify(data)
    })
  }

  // System actions
  async getSystemActions() {
    return this.request('/system-commands')
  }

  // Binary path
  async getBinaryPath() {
    return this.request('/binary-path')
  }

  // Server-Sent Events methods
  connectSSE() {
    if (this.eventSource && this.eventSource.readyState === EventSource.OPEN) {
      return
    }

    const sseUrl = '/events'
    
    try {
      this.eventSource = new EventSource(sseUrl)
      
      this.eventSource.onopen = () => {
        console.log('SSE connected')
        this.reconnectAttempts = 0
      }
      
      this.eventSource.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data)
          this.handleSSEMessage(message)
        } catch (error) {
          console.error('Failed to parse SSE message:', error)
        }
      }
      
      this.eventSource.onerror = (error) => {
        console.error('SSE error:', error)
        this.eventSource.close()
        this.attemptReconnect()
      }
    } catch (error) {
      console.error('Failed to create SSE connection:', error)
      this.attemptReconnect()
    }
  }

  handleSSEMessage(message) {
    const listeners = this.listeners.get(message.type) || []
    listeners.forEach(callback => {
      try {
        callback(message.data)
      } catch (error) {
        console.error('SSE listener error:', error)
      }
    })
  }

  attemptReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.log('Max reconnect attempts reached')
      return
    }

    this.reconnectAttempts++
    const delay = this.reconnectInterval * Math.pow(2, this.reconnectAttempts - 1)
    
    setTimeout(() => {
      console.log(`Attempting to reconnect SSE (attempt ${this.reconnectAttempts})`)
      this.connectSSE()
    }, delay)
  }

  onSSEMessage(type, callback) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, [])
    }
    this.listeners.get(type).push(callback)
    
    // Return unsubscribe function
    return () => {
      const listeners = this.listeners.get(type) || []
      const index = listeners.indexOf(callback)
      if (index > -1) {
        listeners.splice(index, 1)
      }
    }
  }

  disconnectSSE() {
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
  }
}

// Export a singleton instance
export default new ApiClient()