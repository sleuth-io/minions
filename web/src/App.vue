<template>
  <div id="app">
    <header class="header">
      <h1>Coding Agent Dashboard</h1>
      <p class="subtitle">Monitor and manage coding tasks across repositories</p>
    </header>
    
    <main class="main-content">
      <!-- Waiting Tasks Section -->
      <div class="section waiting-tasks" v-if="waitingTasks.length > 0">
        <h2>⏳ Tasks Waiting for Input</h2>
        <div class="task-list">
          <div v-for="task in waitingTasks" :key="task.path" class="task-item">
            <div class="task-info">
              <div class="task-name">{{ task.name }}</div>
              <div class="task-details">
                <span class="task-repo" :title="task.path">{{ task.repository }}</span>
                <span :class="['task-status', task.status]">{{ task.status }}</span>
                <span class="task-time">{{ formatTimeSince(task.last_activity) }}</span>
              </div>
              <div 
                v-if="task.last_message" 
                class="task-message"
                @click="toggleMessageExpansion(task.path)"
                :title="task.full_last_message ? 'Click to expand' : ''"
              >
                {{ task.last_message }}
              </div>
              <div 
                v-if="expandedMessages[task.path] && task.full_last_message" 
                class="expanded-message"
              >
                {{ task.full_last_message }}
                <div class="message-actions">
                  <button @click="sendMinionMessage(task.path, 'hi')" class="action-btn">
                    Hi
                  </button>
                </div>
              </div>
            </div>
            <div class="task-actions">
              <button @click="showMinionCommand(task)" class="minion-btn" title="Show command to run Claude in minion mode">
                🤖 Minion
              </button>
              <button @click="openInPyCharm(task.path)" class="open-btn">
                Open in PyCharm
              </button>
              <button 
                v-if="task.isMainCheckout && !task.hasHooks"
                @click="installHook(task.path)"
                :disabled="hookLoading[task.path]"
                class="install-hook-btn"
                title="Install Claude Code hooks for this repository"
              >
                {{ hookLoading[task.path] ? 'Installing...' : 'Install Hooks' }}
              </button>
              <button 
                v-if="task.isMainCheckout"
                @click="removeRepository(task.repoId)"
                class="remove-btn"
                title="Remove repository"
              >
                ×
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

      <!-- Other Tasks Section -->
      <div class="section other-tasks" v-if="!loading && otherTasks.length > 0">
        <h2>📋 Other Tasks</h2>
        <div class="task-list">
          <div v-for="task in otherTasks" :key="task.path" class="task-item">
            <div class="task-info">
              <div class="task-name">{{ task.name }}</div>
              <div class="task-details">
                <span class="task-repo" :title="task.path">{{ task.repository }}</span>
                <span :class="['task-status', task.status]">{{ task.status }}</span>
                <span v-if="task.last_activity" class="task-time">{{ formatTimeSince(task.last_activity) }}</span>
              </div>
              <div 
                v-if="task.last_message" 
                class="task-message"
                @click="toggleMessageExpansion(task.path)"
                :title="task.full_last_message ? 'Click to expand' : ''"
              >
                {{ task.last_message }}
              </div>
              <div 
                v-if="expandedMessages[task.path] && task.full_last_message" 
                class="expanded-message"
              >
                {{ task.full_last_message }}
                <div class="message-actions">
                  <button @click="sendMinionMessage(task.path, 'hi')" class="action-btn">
                    Hi
                  </button>
                </div>
              </div>
            </div>
            <div class="task-actions">
              <button @click="showMinionCommand(task)" class="minion-btn" title="Show command to run Claude in minion mode">
                🤖 Minion
              </button>
              <button @click="openInPyCharm(task.path)" class="open-btn">
                Open in PyCharm
              </button>
              <button 
                v-if="task.isMainCheckout && !task.hasHooks"
                @click="installHook(task.path)"
                :disabled="hookLoading[task.path]"
                class="install-hook-btn"
                title="Install Claude Code hooks for this repository"
              >
                {{ hookLoading[task.path] ? 'Installing...' : 'Install Hooks' }}
              </button>
              <button 
                v-if="task.isMainCheckout"
                @click="removeRepository(task.repoId)"
                class="remove-btn"
                title="Remove repository"
              >
                ×
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-else-if="!loading && waitingTasks.length === 0" class="empty-state">
        <p>No tasks found. Add a repository to get started.</p>
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

    <!-- Minion Command Dialog -->
    <div v-if="showMinionDialog" class="dialog-overlay" @click="closeMinionDialog">
      <div class="dialog" @click.stop>
        <div class="dialog-header">
          <h3>🤖 Run Claude in Minion Mode</h3>
          <button @click="closeMinionDialog" class="dialog-close">×</button>
        </div>
        <div class="dialog-content">
          <p class="dialog-description">
            Use this command to run Claude in minion mode:
          </p>
          <div class="command-container">
            <code class="command-text" ref="commandText">{{ getMinionCommand(selectedTask) }}</code>
            <button @click="copyCommand" class="copy-btn" title="Copy to clipboard">
              📋 Copy
            </button>
          </div>
          <p class="dialog-note">
            This will connect Claude to the current minion session and allow you to send commands remotely.
          </p>
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
      actionsPanelExpanded: false,
      showMinionDialog: false,
      selectedTask: null,
      binaryPath: null
    }
  },
  async mounted() {
    await this.loadRepositories()
    this.loadPathHistory()
    await this.loadHookStatuses()
    await this.loadSystemActions()
    await this.loadBinaryPath()
    this.setupSSE()
    
    // Note: SSE updates provide real-time data, so no periodic refresh needed
    
    // Refresh when page becomes visible/focused
    this.handleVisibilityChange = () => {
      if (!document.hidden) {
        this.loadRepositories()
        this.loadHookStatuses()
      }
    }
    
    document.addEventListener('visibilitychange', this.handleVisibilityChange)
    window.addEventListener('focus', this.handleVisibilityChange)
  },
  beforeUnmount() {
    apiClient.disconnectSSE()
    if (this.handleVisibilityChange) {
      document.removeEventListener('visibilitychange', this.handleVisibilityChange)
      window.removeEventListener('focus', this.handleVisibilityChange)
    }
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

    allTasks() {
      const tasks = []
      if (!this.repositories) return tasks
      
      for (const repo of this.repositories) {
        const hookStatus = this.getHookStatus(repo.path)
        const addedPaths = new Set()
        
        // Add tasks for all worktrees
        if (repo.worktrees && repo.worktrees.length > 0) {
          for (const worktree of repo.worktrees) {
            // Skip if we've already added a task for this path
            if (addedPaths.has(worktree.path)) continue
            
            const status = repo.status ? repo.status.find(s => s.path === worktree.path) : null
            const taskName = this.getTaskNameFromBranch(worktree.branch) || worktree.branch
            
            tasks.push({
              name: taskName,
              path: worktree.path,
              repository: repo.name || this.getTaskNameFromPath(repo.path),
              repositoryPath: repo.path,
              status: status ? status.status : 'unknown',
              last_activity: status ? status.last_activity : null,
              last_message: status ? status.last_message : null,
              full_last_message: status ? status.full_last_message : null,
              session_id: status ? status.session_id : null,
              isMainCheckout: worktree.path === repo.path,
              hasHooks: hookStatus.is_installed,
              repoId: repo.id
            })
            addedPaths.add(worktree.path)
          }
        }
        
        // Only add main checkout task if not already added as a worktree
        if (!addedPaths.has(repo.path)) {
          const mainStatus = repo.status ? repo.status.find(s => s.path === repo.path) : null
          if (mainStatus || !repo.worktrees || repo.worktrees.length === 0) {
            const mainTaskName = this.getTaskNameFromPath(repo.path) || repo.name || 'main'
            
            const mainTask = {
              name: mainTaskName,
              path: repo.path,
              repository: repo.name || this.getTaskNameFromPath(repo.path),
              repositoryPath: repo.path,
              status: mainStatus ? mainStatus.status : 'unknown',
              last_activity: mainStatus ? mainStatus.last_activity : null,
              last_message: mainStatus ? mainStatus.last_message : null,
              full_last_message: mainStatus ? mainStatus.full_last_message : null,
              session_id: mainStatus ? mainStatus.session_id : null,
              isMainCheckout: true,
              hasHooks: hookStatus.is_installed,
              repoId: repo.id
            }
            tasks.push(mainTask)
          }
        }
      }
      
      return tasks
    },

    waitingTasks() {
      return this.allTasks
        .filter(task => task.status === 'waiting')
        .sort((a, b) => new Date(b.last_activity || 0) - new Date(a.last_activity || 0))
    },

    otherTasks() {
      return this.allTasks
        .filter(task => task.status !== 'waiting')
        .sort((a, b) => new Date(b.last_activity || 0) - new Date(a.last_activity || 0))
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
    

    toggleMessageExpansion(taskPath) {
      this.expandedMessages = {
        ...this.expandedMessages,
        [taskPath]: !this.expandedMessages[taskPath]
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

    async loadBinaryPath() {
      try {
        const response = await apiClient.getBinaryPath()
        this.binaryPath = response.path
      } catch (error) {
        console.error('Failed to load binary path:', error)
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
    },

    getTaskNameFromPath(path) {
      // Extract task name from the repository path
      // For example: /home/user/projects/my-task -> "my-task"
      const parts = path.split('/')
      return parts[parts.length - 1]
    },

    getTaskNameFromBranch(branch) {
      // Extract task name from branch name
      // For example: "feature/user-auth" -> "user-auth"
      //              "bugfix/login-issue" -> "login-issue"
      //              "task/dashboard-rewrite" -> "dashboard-rewrite"
      if (branch.includes('/')) {
        const parts = branch.split('/')
        return parts.slice(1).join('/')
      }
      return branch
    },

    showMinionCommand(task) {
      this.selectedTask = task
      this.showMinionDialog = true
    },

    closeMinionDialog() {
      this.showMinionDialog = false
      this.selectedTask = null
    },

    getMinionCommand(task) {
      if (!task || !this.binaryPath) return ''
      
      // Generate the command to run the current binary in minion mode
      // No need to change directory since current directory is fine
      return `${this.binaryPath} --minion claude`
    },

    async copyCommand() {
      if (!this.selectedTask) return
      
      const command = this.getMinionCommand(this.selectedTask)
      try {
        await navigator.clipboard.writeText(command)
        // Could add a temporary "Copied!" notification here
      } catch (error) {
        console.error('Failed to copy command:', error)
        // Fallback for older browsers
        this.$refs.commandText.select()
        document.execCommand('copy')
      }
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

.waiting-tasks {
  border-left: 4px solid #007bff;
}

.other-tasks {
  border-left: 4px solid #6c757d;
}

.task-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.task-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  background: #f8f9fa;
}

.task-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.task-name {
  font-weight: bold;
  color: #333;
  font-size: 1.1rem;
}

.task-details {
  display: flex;
  gap: 1rem;
  font-size: 0.9rem;
  color: #666;
  align-items: center;
}

.task-repo {
  font-family: monospace;
  background: #e9ecef;
  padding: 0.2rem 0.4rem;
  border-radius: 3px;
}


.task-time {
  color: #007bff;
  font-weight: 500;
}

.task-message {
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

.task-message:hover {
  background: #f1f3f4;
}

.task-actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.task-status {
  padding: 0.25rem 0.5rem;
  border-radius: 12px;
  font-size: 0.8rem;
  font-weight: bold;
  text-transform: uppercase;
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

.task-status.running {
  background: #d4edda;
  color: #155724;
}

.task-status.waiting {
  background: #cce5ff;
  color: #004085;
}

.task-status.idle {
  background: #fff3cd;
  color: #856404;
}

.task-status.paused {
  background: #f8d7da;
  color: #721c24;
}

.task-status.unknown {
  background: #e9ecef;
  color: #6c757d;
}

.minion-btn {
  padding: 0.5rem 1rem;
  background: #6f42c1;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.9rem;
  display: flex;
  align-items: center;
  gap: 0.3rem;
}

.minion-btn:hover {
  background: #5a32a3;
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

/* Dialog Styles */
.dialog-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2000;
}

.dialog {
  background: white;
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  max-width: 600px;
  width: 90%;
  max-height: 80vh;
  overflow: auto;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  border-bottom: 1px solid #e0e0e0;
}

.dialog-header h3 {
  margin: 0;
  color: #333;
  font-size: 1.2rem;
}

.dialog-close {
  background: none;
  border: none;
  font-size: 1.5rem;
  cursor: pointer;
  color: #666;
  padding: 0;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
}

.dialog-close:hover {
  background: #f5f5f5;
  color: #333;
}

.dialog-content {
  padding: 1.5rem;
}

.dialog-description {
  margin-bottom: 1rem;
  color: #555;
  line-height: 1.5;
}

.command-container {
  display: flex;
  gap: 0.5rem;
  margin: 1rem 0;
  align-items: stretch;
}

.command-text {
  flex: 1;
  background: #f8f9fa;
  border: 1px solid #e0e0e0;
  padding: 1rem;
  border-radius: 4px;
  font-family: monospace;
  font-size: 0.9rem;
  color: #333;
  word-break: break-all;
  display: block;
}

.copy-btn {
  padding: 1rem;
  background: #007bff;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.9rem;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 0.3rem;
}

.copy-btn:hover {
  background: #0056b3;
}

.dialog-note {
  margin-top: 1rem;
  padding: 1rem;
  background: #f8f9fa;
  border-radius: 4px;
  color: #666;
  font-size: 0.9rem;
  line-height: 1.4;
}
</style>