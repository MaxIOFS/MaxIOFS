import Swal, { SweetAlertOptions, SweetAlertResult } from 'sweetalert2';

// Configuración por defecto para todos los modales
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

// Wrapper principal para SweetAlert2 con configuración personalizada
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

  // Mostrar un modal genérico
  static async fire(options: SweetAlertOptions): Promise<SweetAlertResult> {
    return Swal.fire(this.mergeConfig(options));
  }

  // Mostrar mensaje de éxito
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

  // Mostrar mensaje de error
  static async error(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'error',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Entendido',
      ...options,
    });
  }

  // Mostrar mensaje de advertencia
  static async warning(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Entendido',
      ...options,
    });
  }

  // Mostrar mensaje informativo
  static async info(title: string, text?: string, options?: SweetAlertOptions): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'info',
      title,
      text,
      showConfirmButton: true,
      confirmButtonText: 'Entendido',
      ...options,
    });
  }

  // Confirmar una acción destructiva
  static async confirmDelete(
    itemName: string,
    itemType: string = 'elemento',
    options?: SweetAlertOptions
  ): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title: `¿Eliminar ${itemType}?`,
      html: `
        <p>Estás a punto de eliminar <strong>"${itemName}"</strong></p>
        <p class="text-red-600 mt-2">Esta acción no se puede deshacer</p>
      `,
      showCancelButton: true,
      confirmButtonText: 'Sí, eliminar',
      cancelButtonText: 'Cancelar',
      confirmButtonColor: '#dc2626',
      reverseButtons: true,
      ...options,
    });
  }

  // Confirmar una acción con entrada de texto
  static async confirmWithInput(
    title: string,
    text: string,
    expectedInput: string,
    inputPlaceholder: string = 'Escribe aquí...',
    options?: SweetAlertOptions
  ): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'warning',
      title,
      html: `
        <p>${text}</p>
        <p class="text-sm text-gray-600 mt-2">Para confirmar, escribe: <strong>${expectedInput}</strong></p>
      `,
      input: 'text',
      inputPlaceholder,
      showCancelButton: true,
      confirmButtonText: 'Confirmar',
      cancelButtonText: 'Cancelar',
      confirmButtonColor: '#dc2626',
      preConfirm: (value) => {
        if (value !== expectedInput) {
          Swal.showValidationMessage(`Debes escribir exactamente: ${expectedInput}`);
          return false;
        }
        return value;
      },
      ...options,
    });
  }

  // Mostrar estado de carga
  static async loading(title: string = 'Cargando...', text?: string): Promise<void> {
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

  // Cerrar cualquier modal abierto
  static close(): void {
    Swal.close();
  }

  // Mostrar toast (notificación ligera)
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

  // Mostrar progreso con barra
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

  // Actualizar barra de progreso
  static updateProgress(percentage: number): void {
    const progressBar = document.getElementById('progress-bar');
    if (progressBar) {
      progressBar.style.width = `${Math.min(100, Math.max(0, percentage))}%`;
    }
  }

  // Helpers específicos para la aplicación MaxIOFS

  // Confirmación específica para eliminar bucket
  static async confirmDeleteBucket(bucketName: string): Promise<SweetAlertResult> {
    return this.confirmWithInput(
      '⚠️ Eliminar Bucket',
      `Estás a punto de eliminar permanentemente el bucket "${bucketName}" y todos sus datos.`,
      bucketName,
      'Nombre del bucket'
    );
  }

  // Confirmación para eliminar objeto
  static async confirmDeleteObject(objectName: string): Promise<SweetAlertResult> {
    return this.confirmDelete(objectName, 'objeto');
  }

  // Confirmación para eliminar usuario
  static async confirmDeleteUser(username: string): Promise<SweetAlertResult> {
    return this.confirmDelete(username, 'usuario');
  }

  // Notificación de éxito para operaciones comunes
  static async successUpload(fileName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Archivo "${fileName}" subido exitosamente`);
  }

  static async successDownload(fileName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Archivo "${fileName}" descargado exitosamente`);
  }

  static async successBucketCreated(bucketName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Bucket "${bucketName}" creado exitosamente`);
  }

  static async successBucketDeleted(bucketName: string): Promise<SweetAlertResult> {
    return this.toast('success', `Bucket "${bucketName}" eliminado exitosamente`);
  }

  static async successUserCreated(username: string): Promise<SweetAlertResult> {
    return this.toast('success', `Usuario "${username}" creado exitosamente`);
  }

  // Manejo de errores comunes de API
  static async apiError(error: any): Promise<SweetAlertResult> {
    let title = 'Error en la operación';
    let text = 'Ha ocurrido un error inesperado';

    if (error?.response?.data?.error) {
      text = error.response.data.error;
    } else if (error?.message) {
      text = error.message;
    } else if (typeof error === 'string') {
      text = error;
    }

    // Personalizar mensajes para errores comunes
    if (text.includes('404')) {
      title = 'Recurso no encontrado';
      text = 'El recurso solicitado no existe o ha sido eliminado';
    } else if (text.includes('401') || text.includes('unauthorized')) {
      title = 'No autorizado';
      text = 'Tu sesión ha expirado. Por favor, inicia sesión nuevamente';
    } else if (text.includes('403') || text.includes('forbidden')) {
      title = 'Acceso denegado';
      text = 'No tienes permisos para realizar esta operación';
    } else if (text.includes('500')) {
      title = 'Error del servidor';
      text = 'Ha ocurrido un error interno del servidor. Inténtalo más tarde';
    } else if (text.includes('network') || text.includes('Network')) {
      title = 'Error de conexión';
      text = 'No se pudo conectar con el servidor. Verifica tu conexión a internet';
    }

    return this.error(title, text);
  }

  // Mostrar confirmación de login exitoso
  static async successLogin(username: string): Promise<SweetAlertResult> {
    return this.toast('success', `¡Bienvenido, ${username}!`);
  }

  // Confirmación de logout
  static async confirmLogout(): Promise<SweetAlertResult> {
    return this.fire({
      icon: 'question',
      title: '¿Cerrar sesión?',
      text: '¿Estás seguro de que quieres cerrar tu sesión?',
      showCancelButton: true,
      confirmButtonText: 'Sí, cerrar sesión',
      cancelButtonText: 'Cancelar',
    });
  }
}

export default SweetAlert;