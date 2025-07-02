class ApiClient {
  constructor(baseURL = '/api') {
    this.baseURL = baseURL
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
}

// Export a singleton instance
export default new ApiClient()