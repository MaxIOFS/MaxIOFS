import Swal, { SweetAlertOptions, SweetAlertResult } from 'sweetalert2';

// Default configuration for all modals
const defaultConfig: SweetAlertOptions = {
  customClass: {
    popup: 'rounded-lg shadow-xl',
    confirmButton: 'bg-blue-600 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded focus:outline-none focus:shadow-outline transition-colors duration-200',
    cancelButton: 'bg-gray-500 hover:bg-gray-600 text-white font-bold py-2 px-4 rounded focus:outline-none focus:shadow-outline transition-colors duration-200 mr-2',
    denyButton: 'bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded focus:outline-none focus:shadow-outline transition-colors duration-200',
  },
  buttonsStyling: false,
  reverseButtons: true,
  focusConfirm: false,
  allowOutsideClick: true,
  allowEscapeKey: true,
};

// Main wrapper for SweetAlert2 with custom configuration
class SweetAlert {
  private static mergeConfig(options: SweetAlertOptions): SweetAlertOptions {
    const merged = { ...defaultConfig };
    
    // Merge all properties except customClass
    Object.keys(options).forEach(key => {
      if (key !== 'customClass') {
        (merged as any)[key] = (options as any)[key];
      }
    });

    // Merge customClass separately
    if (options.customClass) {
      merged.customClass = {
        ...defaultConfig.customClass,
        ...options.customClass,
      };
    }

    return merged;
  }

  // Show a generic modal
  static async fire(options: SweetAlertOptions): Promise<SweetAlertResult> {
    return Swal.fire(this.mergeConfig(options));
  }

  // Show success message
  static async success(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'success',
      title,
      text,
      showConfirmButton: false,
      timer: 2000,
      timerProgressBar: true,
      toast: false,
      ...options,
    });
  }

  // Show error message
  static async error(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'error',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Understood',
      ...options,
    });
  }

  // Show warning message
  static async warning(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Understood',
      ...options,
    });
  }

  // Show info message
  static async info(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'info',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Understood',
      ...options,
    });
  }

  // Confirm a destructive action
  static async confirmDelete(
    itemName: string,
    itemType: string = 'item',
    options?: SweetAlertOptions
  ): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title: `Delete ${itemType}?`,
      html: `
        <p>You are about to delete <strong>"${itemName}"</strong></p>
        <p class="text-red-600 mt-2">This action cannot be undone</p>
      `,
      showCancelButton: true,
      confirmButtonText: 'Yes, delete',
      cancelButtonText: 'Cancel',
      confirmButtonColor: '#dc2626',
      reverseButtons: true,
      ...options,
    });
  }

  // Confirm an action with text input
  static async confirmWithInput(
    title: string,
    text: string,
    expectedInput: string,
    inputPlaceholder: string = 'Type here...',
    options?: SweetAlertOptions
  ): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title,
      html: `
        <p>${text}</p>
        <p class="text-sm text-gray-600 mt-2">To confirm, type: <strong>${expectedInput}</strong></p>
      `,
      input: 'text',
      inputPlaceholder,
      showCancelButton: true,
      confirmButtonText: 'Confirm',
      cancelButtonText: 'Cancel',
      confirmButtonColor: '#dc2626',
      preConfirm: (value) => {
        if (value !== expectedInput) {
          Swal.showValidationMessage(`You must type exactly: ${expectedInput}`);
          return false;
        }
        return value;
      },
      ...options,
    });
  }

  // Show loading state
  static async loading(title: string = 'Loading...', text?: string): Promise<void> {
    Swal.fire({
      title,
      text,
      allowOutsideClick: false,
      allowEscapeKey: false,
      showConfirmButton: false,
      didOpen: () => {
        Swal.showLoading();
      },
    });
  }

  // Close any open modal
  static close(): void {
    Swal.close();
  }

  // Show toast (light notification)
  static async toast(
    icon: 'success' | 'error' | 'warning' | 'info' | 'question',
    title: string,
    position: 'top' | 'top-start' | 'top-end' | 'center' | 'center-start' | 'center-end' | 'bottom' | 'bottom-start' | 'bottom-end' = 'top-end',
    timer: number = 3000
  ): Promise<SweetAlertResult> {
    return Swal.fire({
      icon,
      title,
      toast: true,
      position,
      showConfirmButton: false,
      timer,
      timerProgressBar: true,
      didOpen: (toast) => {
        toast.addEventListener('mouseenter', Swal.stopTimer);
        toast.addEventListener('mouseleave', Swal.resumeTimer);
      },
    });
  }

  // Show progress with bar
  static async progress(title: string, text?: string): Promise<void> {
    await Swal.fire({
      title,
      text,
      allowOutsideClick: false,
      allowEscapeKey: false,
      showConfirmButton: false,
      html: `
        <div class="mb-4">${text || ''}</div>
        <div class="w-full bg-gray-200 rounded-full h-2.5">
          <div id="progress-bar" class="bg-blue-600 h-2.5 rounded-full transition-all duration-300" style="width: 0%"></div>
        </div>
      `,
    });
  }

  // Update progress bar
  static updateProgress(percentage: number): void {
    const progressBar = document.getElementById('progress-bar');
    if (progressBar) {
      progressBar.style.width = `${Math.min(100, Math.max(0, percentage))}%`;
    }
  }

  // Helpers specific to MaxIOFS application

  // Specific confirmation for bucket deletion
  static async confirmDeleteBucket(bucketName: string): Promise<SweetAlertResult> {
    return this.confirmWithInput(
      '⚠️ Delete Bucket',
      `You are about to permanently delete bucket "${bucketName}" and all its data.`,
      bucketName,
      'Bucket name'
    );
  }

  // Confirmation for object deletion
  static async confirmDeleteObject(objectName: string): Promise<SweetAlertResult> {
    return this.confirmDelete(objectName, 'object');
  }

  // Confirmation for user deletion
  static async confirmDeleteUser(username: string): Promise<SweetAlertResult> {
    return this.confirmDelete(username, 'user');
  }

  // Success notifications for common operations
  static async successUpload(fileName: string): Promise<SweetAlertResult> {
    return this.toast('success', `File "${fileName}" uploaded successfully`);
  }

  static async successDownload(fileName: string): Promise<SweetAlertResult> {
    return this.toast('success', `File "${fileName}" downloaded successfully`);
  }

  static async successBucketCreated(bucketName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Bucket "${bucketName}" created successfully`);
  }

  static async successBucketDeleted(bucketName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Bucket "${bucketName}" deleted successfully`);
  }

  static async successUserCreated(username: string): Promise<SweetAlertResult> {
    return this.toast('success', `User "${username}" created successfully`);
  }

  // Handling common API errors
  static async apiError(error: any): Promise<SweetAlertResult> {
    console.log('SweetAlert.apiError - Received error:', error);
    console.log('SweetAlert.apiError - error.details:', error?.details);
    
    let title = 'Operation error';
    let text = 'An unexpected error has occurred';

    // Try different properties where the error message might be
    if (error?.details?.error) {
      text = error.details.error;
      console.log('SweetAlert.apiError - Using error.details.error:', text);
    } else if (error?.details?.Error) {
      text = error.details.Error;
      console.log('SweetAlert.apiError - Using error.details.Error:', text);
    } else if (error?.response?.data?.error) {
      text = error.response.data.error;
      console.log('SweetAlert.apiError - Using error.response.data.error:', text);
    } else if (error?.response?.data?.Error) {
      // Try with capital E (APIResponse struct uses capital E)
      text = error.response.data.Error;
      console.log('SweetAlert.apiError - Using error.response.data.Error:', text);
    } else if (error?.message) {
      text = error.message;
      console.log('SweetAlert.apiError - Using error.message:', text);
    } else if (typeof error === 'string') {
      text = error;
      console.log('SweetAlert.apiError - Using error as string:', text);
    }

    console.log('SweetAlert.apiError - Final text:', text);

    // Customize messages for common errors
    if (text.includes('404')) {
      title = 'Resource not found';
      text = 'The requested resource does not exist or has been deleted';
    } else if (text.includes('401') || text.includes('unauthorized')) {
      title = 'Unauthorized';
      text = 'Your session has expired. Please sign in again';
    } else if (text.includes('403') || text.includes('forbidden') || text.includes('Forbidden')) {
      title = 'Access denied';
      console.log('SweetAlert.apiError - Detected 403/forbidden, checking for retention keywords');
      // Check if it's an Object Lock retention error - keep the detailed message
      if (text.includes('retention') || text.includes('COMPLIANCE') || text.includes('GOVERNANCE') || text.includes('protected by')) {
        console.log('SweetAlert.apiError - Retention error detected, keeping detailed message');
        // Keep the specific error message from backend
        // Don't override with generic message
      } else {
        console.log('SweetAlert.apiError - Generic 403, using default message');
        text = 'You do not have permission to perform this operation';
      }
    } else if (text.includes('500')) {
      title = 'Server error';
      text = 'An internal server error has occurred. Please try again later';
    } else if (text.includes('network') || text.includes('Network')) {
      title = 'Connection error';
      text = 'Could not connect to the server. Check your internet connection';
    }

    console.log('SweetAlert.apiError - Final title:', title, 'Final text:', text);

    return this.error(title, text);
  }

  // Show successful login confirmation
  static async successLogin(username: string): Promise<SweetAlertResult> {
    return this.toast('success', `Welcome, ${username}!`);
  }

  // Logout confirmation
  static async confirmLogout(): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'question',
      title: 'Sign out?',
      text: 'Are you sure you want to sign out?',
      showCancelButton: true,
      confirmButtonText: 'Yes, sign out',
      cancelButtonText: 'Cancel',
    });
  }
}

export default SweetAlert;