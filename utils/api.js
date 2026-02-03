// API请求工具类
class ApiRequest {
  constructor(baseUrl = '') {
    this.baseUrl = baseUrl;
  }

  /**
   * 发送GET请求
   * @param {string} url - 请求URL
   * @param {Object} params - URL参数
   * @returns {Promise} - 返回响应数据
   */
  async get(url, params = {}) {
    try {
      // 构建查询字符串
      const queryString = new URLSearchParams(params).toString();
      const fullUrl = queryString ? `${url}?${queryString}` : url;
      
      const response = await fetch(fullUrl, {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
        },
      });

      return await this.handleResponse(response);
    } catch (error) {
      console.error('GET请求失败:', error);
      throw error;
    }
  }

  /**
   * 发送POST请求
   * @param {string} url - 请求URL
   * @param {Object} data - 请求数据
   * @param {Object} params - URL参数
   * @returns {Promise} - 返回响应数据
   */
  async post(url, data = {}, params = {}) {
    try {
      // 构建查询字符串
      const queryString = new URLSearchParams(params).toString();
      const fullUrl = queryString ? `${url}?${queryString}` : url;
      
      const response = await fetch(fullUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
      });

      return await this.handleResponse(response);
    } catch (error) {
      console.error('POST请求失败:', error);
      throw error;
    }
  }

  /**
   * 处理API响应
   * @param {Response} response - fetch响应对象
   * @returns {Promise} - 返回处理后的数据
   */
  async handleResponse(response) {
    // 尝试解析响应数据
    let responseData;
    try {
      responseData = await response.json();
    } catch (parseError) {
      throw new Error(`响应解析失败: ${parseError.message}`);
    }

    // 检查HTTP状态码
    if (!response.ok) {
      // 如果API返回了错误信息，使用API的错误信息
      if (responseData && responseData.message) {
        throw new Error(responseData.message);
      } else {
        throw new Error(`API请求失败，HTTP状态码: ${response.status}`);
      }
    }

    // 检查API响应格式
    if (responseData && responseData.code !== 200) {
      throw new Error(responseData.message || 'API请求失败');
    }

    // 返回成功的数据
    return responseData;
  }
}

// 创建API请求实例
const apiRequest = new ApiRequest();

// 导出API请求工具
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { ApiRequest, apiRequest };
} else if (typeof window !== 'undefined') {
  window.ApiRequest = ApiRequest;
  window.apiRequest = apiRequest;
}