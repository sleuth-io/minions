<template>
  <div id="app">
    <header class="header">
      <h1>Coding Agent Dashboard</h1>
      <p class="subtitle">Monitor and manage coding agent activities across repositories</p>
    </header>
    
    <main class="main-content">
      <!-- Waiting Agents Section -->
      <div class="section waiting-agents" v-if="waitingAgents.length > 0">
        <h2>⏳ Waiting for User Input</h2>
        <div class="waiting-list">
          <div v-for="agent in waitingAgents" :key="agent.path" class="waiting-item">
            <div class="waiting-info">
              <div class="waiting-path">{{ agent.path }}</div>
              <div class="waiting-details">
                <span class="waiting-session">Session: {{ agent.session_id?.substring(0, 8) }}...</span>
                <span class="waiting-time">{{ formatTimeSince(agent.last_activity) }}</span>
              </div>
              <div 
                v-if="agent.last_message" 
                class="waiting-message"
                @click="toggleMessageExpansion(agent.path)"
                :title="agent.full_last_message ? 'Click to expand' : ''"
              >
                {{ agent.last_message }}
              </div>
              <div 
                v-if="expandedMessages[agent.path] && agent.full_last_message" 
                class="expanded-message"
              >
                {{ agent.full_last_message }}
                <div class="message-actions">
                  <button @click="sendMinionMessage(agent.path, 'hi')" class="action-btn">
                    Hi
                  </button>
                </div>
              </div>
            </div>
            <div class="waiting-actions">
              <button @click="openInPyCharm(agent.path)" class="open-btn">
                Open in PyCharm
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Loading state -->
      <div v-if="loading" class="loading">
        <p>Loading...</p>
      </div>

      <!-- Error state -->
      <div v-if="error" class="error-message">
        <p>{{ error }}</p>
        <button @click="loadRepositories" class="retry-btn">Retry</button>
      </div>

      <!-- Repositories Section -->
      <div class="section repositories" v-if="!loading && repositories.length > 0">
        <h2>📁 Repositories</h2>
        <div v-for="repo in repositories" :key="repo.id" class="repository-card">
          <div class="repo-header">
            <div class="repo-info">
              <h3>{{ repo.name || repo.path }}</h3>
              <span class="repo-path" v-if="repo.name">{{ repo.path }}</span>
            </div>
            <div class="repo-actions">
              <div class="hook-status">
                <span 
                  v-if="getHookStatus(repo.path).is_installed" 
                  class="hook-indicator installed"
                  title="Claude Code hooks are installed"
                >
                  🪝 Hooks Installed
                </span>
                <div v-else class="hook-not-installed">
                  <span class="hook-indicator not-installed" title="Claude Code hooks not installed">
                    🚫 No Hooks
                  </span>
                  <button 
                    @click="installHook(repo.path)"
                    :disabled="hookLoading[repo.path]"
                    class="install-hook-btn"
                    title="Install Claude Code hooks for this repository"
                  >
                    {{ hookLoading[repo.path] ? 'Installing...' : 'Install Hooks' }}
                  </button>
                </div>
              </div>
              <button @click="removeRepository(repo.id)" class="remove-btn">×</button>
            </div>
          </div>
          
          <div class="worktrees" v-if="repo.worktrees && repo.worktrees.length > 0">
            <div v-for="worktree in repo.worktrees" :key="worktree.path" class="worktree-item">
              <div class="worktree-info">
                <span class="branch-name">{{ worktree.branch }}</span>
                <span class="worktree-path">{{ worktree.path }}</span>
                <span :class="['status', getWorktreeStatus(worktree)]">{{ getWorktreeStatus(worktree) }}</span>
                <span 
                  v-if="getWorktreeLastMessage(worktree)" 
                  class="last-message"
                  @click="toggleMessageExpansion(worktree.path)"
                  :title="getWorktreeFullLastMessage(worktree) ? 'Click to expand' : ''"
                >
                  {{ getWorktreeLastMessage(worktree) }}
                </span>
                <div 
                  v-if="expandedMessages[worktree.path] && getWorktreeFullLastMessage(worktree)" 
                  class="expanded-message"
                >
                  {{ getWorktreeFullLastMessage(worktree) }}
                  <div class="message-actions">
                    <button @click="sendMinionMessage(worktree.path, 'hi')" class="action-btn">
                      Hi
                    </button>
                  </div>
                </div>
              </div>
              <button @click="openInPyCharm(worktree.path)" class="open-btn">
                Open in PyCharm
              </button>
            </div>
          </div>
          
          <div v-else class="no-worktrees">
            <p>No worktrees found for this repository</p>
          </div>
        </div>
      </div>

      <div v-else-if="!loading" class="empty-state">
        <p>No repositories configured. Add a repository to get started.</p>
      </div>

      <!-- Add Repository Section -->
      <div class="section add-repository">
        <h2>➕ Add Repository</h2>
        <div class="input-container">
          <div class="path-input-group">
            <input 
              ref="pathInput"
              v-model="newRepoPath" 
              type="text" 
              placeholder="Enter repository path (e.g., /home/user/projects/my-repo)" 
              class="repo-input"
              @input="onPathInput"
              @keyup.enter="addRepository"
              @focus="showSuggestions = true"
              @blur="hideSuggestions"
              autocomplete="off"
            />
            <button @click="addRepository" :disabled="!newRepoPath.trim()" class="add-btn">
              Add Repository
            </button>
          </div>
          
          <!-- Autocomplete suggestions -->
          <div v-if="showSuggestions && (directorySuggestions.length > 0 || localSuggestions.length > 0 || suggestionLoading)" class="suggestions-dropdown">
            <!-- Loading indicator -->
            <div v-if="suggestionLoading" class="suggestion-loading">
              <span>Loading directories...</span>
            </div>
            
            <!-- Directory suggestions from backend -->
            <div 
              v-for="(suggestion, index) in directorySuggestions" 
              :key="'dir-' + index"
              class="suggestion-item"
              @mousedown="selectSuggestion(suggestion)"
            >
              <span class="suggestion-path">{{ suggestion.path }}</span>
              <div class="suggestion-badges">
                <span v-if="suggestion.is_git_repo" class="suggestion-badge git-repo">Git Repo</span>
                <span v-else-if="suggestion.has_git_repos" class="suggestion-badge has-git">Has Git Repos</span>
                <span class="suggestion-badge type">{{ suggestion.type }}</span>
              </div>
            </div>
            
            <!-- Local history suggestions -->
            <div 
              v-for="(suggestion, index) in localSuggestions" 
              :key="'local-' + index"
              class="suggestion-item"
              @mousedown="selectSuggestion(suggestion)"
            >
              <span class="suggestion-path">{{ suggestion.path }}</span>
              <div class="suggestion-badges">
                <span class="suggestion-badge type">{{ suggestion.type }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- System Actions Panel -->
    <div class="actions-panel" :class="{ expanded: actionsPanelExpanded }">
      <div class="actions-header" @click="toggleActionsPanel">
        <h3>🔧 System Actions</h3>
        <div class="actions-info">
          <span class="action-count">{{ systemActions.length }} actions</span>
          <span class="expand-icon" :class="{ rotated: actionsPanelExpanded }">▼</span>
        </div>
      </div>
      
      <div class="actions-content" v-if="actionsPanelExpanded">
        <div v-if="systemActions.length === 0" class="no-actions">
          No system actions executed yet
        </div>
        <div v-else class="actions-list">
          <div 
            v-for="action in systemActions" 
            :key="action.id" 
            class="action-item"
            :class="'type-' + action.type"
          >
            <div class="action-main">
              <div class="action-text">
                <span class="action-description">{{ action.description }}</span>
                <span v-if="action.command" class="action-command">{{ action.command }}</span>
              </div>
              <div class="action-meta">
                <span class="action-time">{{ formatTimestamp(action.timestamp) }}</span>
                <span class="action-type" :class="'type-' + action.type">{{ action.type }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import apiClient from './api/client.js'

export default {
  name: 'App',
  data() {
    return {
      newRepoPath: '',
      repositories: [],
      loading: false,
      error: null,
      showSuggestions: false,
      pathHistory: [],
      directorySuggestions: [],
      suggestionLoading: false,
      hookStatuses: {},
      hookLoading: {},
      expandedMessages: {},
      systemActions: [],
      actionsPanelExpanded: false
    }
  },
  async mounted() {
    await this.loadRepositories()
    this.loadPathHistory()
    await this.loadHookStatuses()
    await this.loadSystemActions()
    this.setupSSE()
  },
  beforeUnmount() {
    apiClient.disconnectSSE()
  },
  computed: {
    localSuggestions() {
      if (!this.newRepoPath || this.newRepoPath.length < 2) return []
      
      const query = this.newRepoPath.toLowerCase()
      const suggestions = []
      
      // Add path history matches
      this.pathHistory.forEach(path => {
        if (path.toLowerCase().includes(query)) {
          suggestions.push({ path, type: 'history', name: path, is_git_repo: false })
        }
      })
      
      return suggestions
    },

    waitingAgents() {
      const waiting = []
      if (!this.repositories) return waiting
      
      for (const repo of this.repositories) {
        if (repo.status) {
          for (const status of repo.status) {
            if (status.status === 'waiting') {
              waiting.push(status)
            }
          }
        }
      }
      
      // Sort by most recent activity
      return waiting.sort((a, b) => new Date(b.last_activity) - new Date(a.last_activity))
    }
  },
  methods: {
    async loadRepositories() {
      this.loading = true
      this.error = null
      
      try {
        const repos = await apiClient.getRepositories()
        this.repositories = repos || []
      } catch (error) {
        this.error = `Failed to load repositories: ${error.message}`
        console.error('Failed to load repositories:', error)
      } finally {
        this.loading = false
      }
    },
    
    loadPathHistory() {
      const saved = localStorage.getItem('coding-agent-dashboard-path-history')
      if (saved) {
        try {
          this.pathHistory = JSON.parse(saved)
        } catch (e) {
          this.pathHistory = []
        }
      }
    },
    
    savePathHistory() {
      localStorage.setItem('coding-agent-dashboard-path-history', JSON.stringify(this.pathHistory))
    },
    
    async onPathInput() {
      this.showSuggestions = true
      
      if (this.newRepoPath.length >= 2) {
        this.suggestionLoading = true
        try {
          this.directorySuggestions = await apiClient.getDirectorySuggestions(this.newRepoPath)
        } catch (error) {
          console.error('Failed to get directory suggestions:', error)
          this.directorySuggestions = []
        } finally {
          this.suggestionLoading = false
        }
      } else {
        this.directorySuggestions = []
      }
    },
    
    selectSuggestion(suggestion) {
      this.newRepoPath = suggestion.path
      this.showSuggestions = false
      this.$refs.pathInput.focus()
    },
    
    hideSuggestions() {
      // Delay hiding to allow for click events
      setTimeout(() => {
        this.showSuggestions = false
      }, 200)
    },
    
    async addRepository() {
      const path = this.newRepoPath.trim()
      if (!path) return
      
      this.loading = true
      this.error = null
      
      try {
        await apiClient.addRepository(path)
        
        // Add to path history
        if (!this.pathHistory.includes(path)) {
          this.pathHistory.unshift(path)
          this.pathHistory = this.pathHistory.slice(0, 10) // Keep only last 10
          this.savePathHistory()
        }
        
        this.newRepoPath = ''
        this.showSuggestions = false
        await this.loadRepositories() // Reload to get updated data with worktrees
        await this.loadHookStatuses() // Load hook statuses for new repository
      } catch (error) {
        this.error = `Failed to add repository: ${error.message}`
        console.error('Failed to add repository:', error)
      } finally {
        this.loading = false
      }
    },
    
    async removeRepository(id) {
      if (!confirm('Are you sure you want to remove this repository?')) return
      
      this.loading = true
      this.error = null
      
      try {
        await apiClient.removeRepository(id)
        await this.loadRepositories() // Reload the list
      } catch (error) {
        this.error = `Failed to remove repository: ${error.message}`
        console.error('Failed to remove repository:', error)
      } finally {
        this.loading = false
      }
    },
    
    async openInPyCharm(path) {
      try {
        const response = await apiClient.openInIDE(path)
        if (response.status === 'opened') {
          console.log('PyCharm opened successfully for:', path)
        }
      } catch (error) {
        console.error('Failed to open in PyCharm:', error)
        alert(`Failed to open PyCharm: ${error.message}`)
      }
    },
    
    getWorktreeStatus(worktree) {
      // Find status for this worktree from the repository's status array
      if (!this.repositories) return 'unknown'
      
      const repo = this.repositories.find(r => 
        r.worktrees && r.worktrees.some(wt => wt.path === worktree.path)
      )
      
      if (repo && repo.status) {
        const status = repo.status.find(s => s.path === worktree.path)
        return status ? status.status : 'unknown'
      }
      
      return 'unknown'
    },
    
    getWorktreeLastMessage(worktree) {
      // Find last message for this worktree from the repository's status array
      if (!this.repositories) return ''
      
      const repo = this.repositories.find(r => 
        r.worktrees && r.worktrees.some(wt => wt.path === worktree.path)
      )
      
      if (repo && repo.status) {
        const status = repo.status.find(s => s.path === worktree.path)
        return status ? status.last_message || '' : ''
      }
      
      return ''
    },

    getWorktreeFullLastMessage(worktree) {
      // Find full last message for this worktree from the repository's status array
      if (!this.repositories) return ''
      
      const repo = this.repositories.find(r => 
        r.worktrees && r.worktrees.some(wt => wt.path === worktree.path)
      )
      
      if (repo && repo.status) {
        const status = repo.status.find(s => s.path === worktree.path)
        return status ? status.full_last_message || '' : ''
      }
      
      return ''
    },

    toggleMessageExpansion(worktreePath) {
      this.expandedMessages = {
        ...this.expandedMessages,
        [worktreePath]: !this.expandedMessages[worktreePath]
      }
    },

    async sendMinionMessage(path, message) {
      try {
        const response = await fetch('/api/minion/message', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            path: path,
            message: message
          })
        })
        
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }
        
        console.log(`Sent message "${message}" to minion at ${path}`)
      } catch (error) {
        console.error('Failed to send minion message:', error)
      }
    },

    formatTimeSince(timestamp) {
      if (!timestamp) return ''
      
      const now = new Date()
      const time = new Date(timestamp)
      const diffMs = now - time
      const diffMins = Math.floor(diffMs / 60000)
      const diffHours = Math.floor(diffMins / 60)
      const diffDays = Math.floor(diffHours / 24)
      
      if (diffMins < 1) return 'just now'
      if (diffMins < 60) return `${diffMins}m ago`
      if (diffHours < 24) return `${diffHours}h ago`
      return `${diffDays}d ago`
    },
    
    async loadHookStatuses() {
      for (const repo of this.repositories) {
        try {
          const hookStatus = await apiClient.getHookStatus(repo.path)
          this.hookStatuses = { ...this.hookStatuses, [repo.path]: hookStatus }
        } catch (error) {
          console.error('Failed to load hook status for', repo.path, error)
        }
      }
    },
    
    async installHook(repoPath) {
      console.log('Installing hook for:', repoPath)
      this.hookLoading = { ...this.hookLoading, [repoPath]: true }
      
      try {
        console.log('Calling API to install hook...')
        const result = await apiClient.installHook(repoPath)
        console.log('Hook installation result:', result)
        
        // Reload hook status for this repository
        console.log('Reloading hook status...')
        const hookStatus = await apiClient.getHookStatus(repoPath)
        console.log('Hook status after installation:', hookStatus)
        this.hookStatuses = { ...this.hookStatuses, [repoPath]: hookStatus }
        
        // Show success message
        alert('Claude Code hook installed successfully!\n\nThe hook configuration has been added to .claude/settings.local.json and .gitignore has been updated.')
        
      } catch (error) {
        console.error('Failed to install hook:', error)
        alert(`Failed to install hook: ${error.message}`)
      } finally {
        this.hookLoading = { ...this.hookLoading, [repoPath]: false }
      }
    },
    
    getHookStatus(repoPath) {
      return this.hookStatuses[repoPath] || { is_installed: false }
    },
    
    async loadSystemActions() {
      try {
        this.systemActions = await apiClient.getSystemActions()
      } catch (error) {
        console.error('Failed to load system actions:', error)
      }
    },

    setupSSE() {
      // Connect to Server-Sent Events
      apiClient.connectSSE()
      
      // Listen for status updates
      apiClient.onSSEMessage('status_update', (statusData) => {
        console.log('Received status update:', statusData)
        // Update repository status in place without full reload
        this.updateRepositoryStatuses(statusData)
      })
      
      // Listen for action updates
      apiClient.onSSEMessage('actions_update', (actionsData) => {
        console.log('Received actions update:', actionsData)
        this.systemActions = actionsData
      })
    },
    
    updateRepositoryStatuses(newStatuses) {
      // Update the status data for each repository
      this.repositories = this.repositories.map(repo => {
        if (repo.status) {
          // Update existing status entries
          const updatedStatus = repo.status.map(status => {
            const newStatus = newStatuses.find(ns => ns.path === status.path)
            return newStatus || status
          })
          
          // Add any new status entries for this repo's paths
          const repoWorktreePaths = repo.worktrees ? repo.worktrees.map(wt => wt.path) : []
          newStatuses.forEach(newStatus => {
            if (repoWorktreePaths.includes(newStatus.path) && 
                !updatedStatus.find(s => s.path === newStatus.path)) {
              updatedStatus.push(newStatus)
            }
          })
          
          return { ...repo, status: updatedStatus }
        }
        return repo
      })
    },

    toggleActionsPanel() {
      this.actionsPanelExpanded = !this.actionsPanelExpanded
    },

    formatTimestamp(timestamp) {
      const date = new Date(timestamp)
      return date.toLocaleTimeString()
    }
  }
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background-color: #f5f5f5;
  min-height: 100vh;
}

.header {
  background: white;
  padding: 2rem;
  border-bottom: 1px solid #e0e0e0;
  margin-bottom: 2rem;
}

.header h1 {
  color: #333;
  margin-bottom: 0.5rem;
}

.subtitle {
  color: #666;
  font-size: 1.1rem;
}

.main-content {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 2rem 60px; /* Add bottom padding for commands panel */
}

.section {
  background: white;
  padding: 2rem;
  border-radius: 8px;
  margin-bottom: 2rem;
  box-shadow: 0 2px 4px rgba(0,0,0,0.1);
}

.waiting-agents {
  border-left: 4px solid #007bff;
}

.waiting-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.waiting-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  background: #f8f9fa;
}

.waiting-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.waiting-path {
  font-weight: bold;
  font-family: monospace;
  color: #333;
  font-size: 1.1rem;
}

.waiting-details {
  display: flex;
  gap: 1rem;
  font-size: 0.9rem;
  color: #666;
}

.waiting-session {
  font-family: monospace;
  background: #e9ecef;
  padding: 0.2rem 0.4rem;
  border-radius: 3px;
}

.waiting-time {
  color: #007bff;
  font-weight: 500;
}

.waiting-message {
  font-family: monospace;
  color: #495057;
  font-size: 0.9rem;
  background: #fff;
  padding: 0.5rem;
  border-radius: 4px;
  max-width: 500px;
  cursor: pointer;
  transition: background-color 0.2s;
  border-left: 3px solid #007bff;
}

.waiting-message:hover {
  background: #f1f3f4;
}

.waiting-actions {
  display: flex;
  gap: 0.5rem;
}

.add-repository h2 {
  margin-bottom: 1rem;
  color: #333;
}

.input-group {
  display: flex;
  gap: 1rem;
}

.repo-input {
  flex: 1;
  padding: 0.75rem;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 1rem;
}

.add-btn {
  padding: 0.75rem 1.5rem;
  background: #007bff;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 1rem;
}

.add-btn:hover {
  background: #0056b3;
}

.repositories h2 {
  margin-bottom: 1rem;
  color: #333;
}

.repository-card {
  background: white;
  border-radius: 8px;
  margin-bottom: 1.5rem;
  box-shadow: 0 2px 4px rgba(0,0,0,0.1);
  overflow: hidden;
}

.repo-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  background: #f8f9fa;
  border-bottom: 1px solid #e0e0e0;
}

.repo-info h3 {
  color: #333;
  font-family: monospace;
  margin: 0;
}

.repo-actions {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.hook-status {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.hook-not-installed {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.hook-indicator {
  font-size: 0.9rem;
  padding: 0.25rem 0.5rem;
  border-radius: 12px;
  font-weight: bold;
}

.hook-indicator.installed {
  background: #d4edda;
  color: #155724;
}

.hook-indicator.not-installed {
  background: #f8d7da;
  color: #721c24;
}

.install-hook-btn {
  padding: 0.5rem 1rem;
  background: #007bff;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.8rem;
  white-space: nowrap;
}

.install-hook-btn:hover:not(:disabled) {
  background: #0056b3;
}

.install-hook-btn:disabled {
  background: #6c757d;
  cursor: not-allowed;
}

.remove-btn {
  background: #dc3545;
  color: white;
  border: none;
  border-radius: 50%;
  width: 30px;
  height: 30px;
  cursor: pointer;
  font-size: 1.2rem;
  display: flex;
  align-items: center;
  justify-content: center;
}

.remove-btn:hover {
  background: #c82333;
}

.worktrees {
  padding: 1rem;
}

.worktree-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem;
  border: 1px solid #e0e0e0;
  border-radius: 4px;
  margin-bottom: 0.5rem;
}

.worktree-info {
  display: flex;
  gap: 1rem;
  align-items: center;
  flex: 1;
}

.branch-name {
  font-weight: bold;
  color: #333;
}

.worktree-path {
  font-family: monospace;
  color: #666;
  font-size: 0.9rem;
}

.status {
  padding: 0.25rem 0.5rem;
  border-radius: 12px;
  font-size: 0.8rem;
  font-weight: bold;
  text-transform: uppercase;
}

.status.running {
  background: #d4edda;
  color: #155724;
}

.status.waiting {
  background: #cce5ff;
  color: #004085;
}

.status.idle {
  background: #fff3cd;
  color: #856404;
}

.status.paused {
  background: #f8d7da;
  color: #721c24;
}

.status.unknown {
  background: #e9ecef;
  color: #6c757d;
}

.last-message {
  font-family: monospace;
  color: #495057;
  font-size: 0.8rem;
  background: #f8f9fa;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  transition: background-color 0.2s;
}

.last-message:hover {
  background: #e9ecef;
}

.expanded-message {
  font-family: monospace;
  color: #495057;
  font-size: 0.8rem;
  background: #f1f3f4;
  padding: 0.5rem;
  border-radius: 4px;
  margin-top: 0.5rem;
  white-space: pre-wrap;
  word-wrap: break-word;
  max-width: 500px;
  border-left: 3px solid #007bff;
}

.message-actions {
  margin-top: 0.5rem;
  padding-top: 0.5rem;
  border-top: 1px solid #dee2e6;
}

.action-btn {
  padding: 0.25rem 0.5rem;
  background: #007bff;
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 0.75rem;
  cursor: pointer;
  transition: background-color 0.2s;
}

.action-btn:hover {
  background: #0056b3;
}

.open-btn {
  padding: 0.5rem 1rem;
  background: #28a745;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.9rem;
}

.open-btn:hover {
  background: #1e7e34;
}

.empty-state {
  text-align: center;
  padding: 3rem;
  color: #666;
}

.empty-state p {
  font-size: 1.1rem;
}

.loading {
  text-align: center;
  padding: 2rem;
  color: #666;
}

.error-message {
  background: #f8d7da;
  color: #721c24;
  padding: 1rem;
  border-radius: 4px;
  margin-bottom: 1rem;
}

.retry-btn {
  margin-left: 1rem;
  padding: 0.5rem 1rem;
  background: #dc3545;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
}

.retry-btn:hover {
  background: #c82333;
}

.repo-path {
  font-family: monospace;
  font-size: 0.9rem;
  color: #666;
  margin-left: 0.5rem;
}

.no-worktrees {
  padding: 1rem;
  text-align: center;
  color: #666;
  font-style: italic;
}

.input-container {
  position: relative;
  width: 100%;
}

.path-input-group {
  display: flex;
  gap: 1rem;
  margin-bottom: 1rem;
}

.repo-input {
  flex: 1;
  padding: 0.75rem;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 1rem;
  font-family: monospace;
}

.suggestions-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  background: white;
  border: 1px solid #ddd;
  border-radius: 4px;
  box-shadow: 0 4px 8px rgba(0,0,0,0.1);
  z-index: 1000;
  max-height: 200px;
  overflow-y: auto;
}

.suggestion-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem;
  cursor: pointer;
  border-bottom: 1px solid #f0f0f0;
}

.suggestion-item:hover {
  background: #f8f9fa;
}

.suggestion-item:last-child {
  border-bottom: none;
}

.suggestion-path {
  font-family: monospace;
  color: #333;
  flex: 1;
}

.suggestion-badges {
  display: flex;
  gap: 0.25rem;
  align-items: center;
}

.suggestion-badge {
  font-size: 0.7rem;
  padding: 0.2rem 0.4rem;
  border-radius: 8px;
  font-weight: bold;
  text-transform: uppercase;
}

.suggestion-badge.git-repo {
  background: #d4edda;
  color: #155724;
}

.suggestion-badge.has-git {
  background: #fff3cd;
  color: #856404;
}

.suggestion-badge.type {
  background: #e9ecef;
  color: #666;
}

.suggestion-loading {
  padding: 0.75rem;
  text-align: center;
  color: #666;
  font-style: italic;
}

.quick-suggestions {
  margin-top: 1rem;
}

.quick-suggestions h4 {
  margin-bottom: 0.5rem;
  color: #666;
  font-size: 0.9rem;
}

.suggestion-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.suggestion-chip {
  background: #e9ecef;
  border: none;
  padding: 0.5rem 1rem;
  border-radius: 20px;
  cursor: pointer;
  font-size: 0.9rem;
  color: #495057;
}

.suggestion-chip:hover {
  background: #dee2e6;
}

.add-btn:disabled {
  background: #ccc;
  cursor: not-allowed;
}

.add-btn:disabled:hover {
  background: #ccc;
}

/* System Actions Panel */
.actions-panel {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  background: white;
  border-top: 1px solid #e0e0e0;
  box-shadow: 0 -2px 4px rgba(0,0,0,0.1);
  transition: height 0.3s ease;
  z-index: 1000;
  height: 50px;
  overflow: hidden;
}

.actions-panel.expanded {
  height: 300px;
}

.actions-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem 2rem;
  cursor: pointer;
  user-select: none;
  height: 50px;
  box-sizing: border-box;
}

.actions-header:hover {
  background: #f8f9fa;
}

.actions-header h3 {
  margin: 0;
  color: #333;
  font-size: 1rem;
}

.actions-info {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.action-count {
  font-size: 0.9rem;
  color: #666;
  background: #e9ecef;
  padding: 0.25rem 0.5rem;
  border-radius: 12px;
}

.expand-icon {
  font-size: 0.8rem;
  color: #666;
  transition: transform 0.3s ease;
}

.expand-icon.rotated {
  transform: rotate(180deg);
}

.actions-content {
  height: 250px;
  overflow-y: auto;
  padding: 0 2rem 1rem;
}

.no-actions {
  text-align: center;
  color: #666;
  font-style: italic;
  padding: 2rem;
}

.actions-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.action-item {
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  padding: 1rem;
  background: #f8f9fa;
}

.action-item.type-command {
  border-left: 4px solid #007bff;
}

.action-item.type-file_operation {
  border-left: 4px solid #6f42c1;
}

.action-main {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
}

.action-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.action-description {
  font-weight: bold;
  color: #333;
  font-size: 0.95rem;
}

.action-command {
  font-family: monospace;
  color: #495057;
  font-size: 0.85rem;
  background: #f8f9fa;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  border-left: 3px solid #007bff;
}

.action-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.25rem;
}

.action-time {
  font-size: 0.8rem;
  color: #666;
}

.action-type {
  font-size: 0.7rem;
  font-weight: bold;
  text-transform: uppercase;
  padding: 0.2rem 0.5rem;
  border-radius: 10px;
}

.action-type.type-command {
  background: #cce5ff;
  color: #004085;
}

.action-type.type-file_operation {
  background: #e2d3f3;
  color: #6f42c1;
}
</style>